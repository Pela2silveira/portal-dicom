package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// seriesCounts maps a SeriesInstanceUID to its instance count.
type seriesCounts map[string]int

// studyCompletenessReport summarizes how a locally cached study compares against
// the series/instances the source PACS reports for the same study.
type studyCompletenessReport struct {
	// Evaluated is false when expected counts could not be obtained from the
	// source; callers must then degrade to best-effort (treat as complete)
	// instead of raising a false partial.
	Evaluated         bool
	Complete          bool
	ExpectedSeries    int
	PresentSeries     int
	ExpectedInstances int
	PresentInstances  int
	// MissingSeries lists SeriesInstanceUIDs that are absent locally or hold
	// fewer instances than the source reports. Sorted for deterministic output.
	MissingSeries []string
}

// evaluateStudyCompleteness compares expected vs. local per-series instance
// counts. A series is incomplete when it is missing locally or, when the source
// reported an instance count, when the local count is lower. When expected is
// empty the evaluation is skipped (Evaluated=false) so completeness cannot be
// asserted from insufficient data.
func evaluateStudyCompleteness(expected, local seriesCounts) studyCompletenessReport {
	report := studyCompletenessReport{}
	if len(expected) == 0 {
		return report
	}
	report.Evaluated = true
	report.ExpectedSeries = len(expected)

	missing := make([]string, 0)
	for seriesUID, expectedInstances := range expected {
		if expectedInstances > 0 {
			report.ExpectedInstances += expectedInstances
		}
		localInstances, present := local[seriesUID]
		if present {
			report.PresentSeries++
			if localInstances > 0 {
				report.PresentInstances += localInstances
			}
		}
		if !present || (expectedInstances > 0 && localInstances < expectedInstances) {
			missing = append(missing, seriesUID)
		}
	}
	sort.Strings(missing)
	report.MissingSeries = missing
	report.Complete = len(missing) == 0
	return report
}

// orthancFindSeriesResource is the Series-level shape returned by POST
// /tools/find with Expand=true; Instances carries one entry per stored instance.
type orthancFindSeriesResource struct {
	MainDicomTags struct {
		SeriesInstanceUID string `json:"SeriesInstanceUID"`
	} `json:"MainDicomTags"`
	Instances []string `json:"Instances"`
}

// localStudySeriesCounts returns the per-series instance counts currently stored
// in the local Orthanc for the given study, using POST /tools/find (never QIDO,
// to avoid the authorization-plugin 403 on unknown resources).
func (a *App) localStudySeriesCounts(ctx context.Context, studyUID string) (seriesCounts, error) {
	body, err := json.Marshal(map[string]any{
		"Level":  "Series",
		"Query":  map[string]string{"StudyInstanceUID": strings.TrimSpace(studyUID)},
		"Expand": true,
	})
	if err != nil {
		return nil, fmt.Errorf("encode local series find body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.OrthancURL, "/")+"/tools/find", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("build local series find request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute local series find request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("local series find bad status %d: %s", res.StatusCode, strings.TrimSpace(string(errBody)))
	}

	var payload []orthancFindSeriesResource
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("decode local series find response: %w", err)
		}
		payload = []orthancFindSeriesResource{}
	}

	counts := make(seriesCounts, len(payload))
	for _, series := range payload {
		uid := strings.TrimSpace(series.MainDicomTags.SeriesInstanceUID)
		if uid == "" {
			continue
		}
		counts[uid] = len(series.Instances)
	}
	return counts, nil
}

// sourceStudySeriesCounts returns the per-series instance counts the source PACS
// reports for the study, branching on the node search protocol. A nil map with a
// nil error means the source could not provide counts (best-effort skip).
func (a *App) sourceStudySeriesCounts(ctx context.Context, node PACSNodeConfig, studyUID string) (seriesCounts, error) {
	resolved := node.Resolved()
	switch strings.ToLower(resolved.SearchMode) {
	case "c_find":
		return a.sourceStudySeriesCountsCFind(ctx, node, studyUID)
	case "qido_rs":
		return a.sourceStudySeriesCountsQIDO(ctx, node, studyUID)
	default:
		return nil, nil
	}
}

func (a *App) sourceStudySeriesCountsCFind(ctx context.Context, node PACSNodeConfig, studyUID string) (seriesCounts, error) {
	resolved := node.Resolved()
	if err := a.ensureOrthancModality(ctx, node); err != nil {
		return nil, fmt.Errorf("orthanc modality ensure failed: %w", err)
	}

	queryPayload, err := json.Marshal(map[string]any{
		"Level": "Series",
		"Query": map[string]string{
			"StudyInstanceUID":               strings.TrimSpace(studyUID),
			"SeriesInstanceUID":              "",
			"Modality":                       "",
			"NumberOfSeriesRelatedInstances": "",
		},
		"Timeout": 60,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.OrthancURL, "/")+"/modalities/"+url.PathEscape(resolved.ID)+"/query", strings.NewReader(string(queryPayload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("orthanc series c-find bad status %d: %s", res.StatusCode, strings.TrimSpace(string(errBody)))
	}

	queryID, err := decodeOrthancQueryID(res.Body)
	if err != nil {
		return nil, err
	}
	answerIDs, err := a.fetchOrthancQueryAnswerIDs(ctx, queryID)
	if err != nil {
		return nil, err
	}

	counts := make(seriesCounts, len(answerIDs))
	for _, answerID := range answerIDs {
		item, err := a.fetchOrthancQueryAnswerContent(ctx, queryID, answerID)
		if err != nil {
			return nil, err
		}
		uid := dicomFirstString(item, "0020000E")
		if uid == "" {
			continue
		}
		counts[uid] = dicomFirstInt(item, "00201209")
	}
	return counts, nil
}

func (a *App) sourceStudySeriesCountsQIDO(ctx context.Context, node PACSNodeConfig, studyUID string) (seriesCounts, error) {
	resolved := node.Resolved()
	token, err := a.fetchPACSBearerToken(ctx, node)
	if err != nil {
		return nil, fmt.Errorf("fetch pacs token for %s: %w", node.ID, err)
	}

	endpoint, err := url.Parse(strings.TrimRight(resolved.DICOMwebBaseURL, "/") + "/series")
	if err != nil {
		return nil, fmt.Errorf("build qido series url: %w", err)
	}
	query := endpoint.Query()
	query.Set("StudyInstanceUID", strings.TrimSpace(studyUID))
	query.Add("includefield", "SeriesInstanceUID")
	query.Add("includefield", "NumberOfSeriesRelatedInstances")
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("qido series bad status %d: %s", res.StatusCode, strings.TrimSpace(string(errBody)))
	}

	var payload []qidoResponseItem
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("decode qido series response: %w", err)
		}
		payload = []qidoResponseItem{}
	}

	counts := make(seriesCounts, len(payload))
	for _, item := range payload {
		uid := dicomFirstString(item, "0020000E")
		if uid == "" {
			continue
		}
		counts[uid] = dicomFirstInt(item, "00201209")
	}
	return counts, nil
}

// verifyRetrievedStudyCompleteness compares the source and local series for a
// freshly retrieved study. It never fails the retrieve on its own: any lookup
// error is reported so the caller can fall back to the legacy behavior.
func (a *App) verifyRetrievedStudyCompleteness(ctx context.Context, node PACSNodeConfig, studyUID string) (studyCompletenessReport, error) {
	expected, err := a.sourceStudySeriesCounts(ctx, node, studyUID)
	if err != nil {
		return studyCompletenessReport{}, fmt.Errorf("source series counts: %w", err)
	}
	if len(expected) == 0 {
		return studyCompletenessReport{}, nil
	}
	local, err := a.localStudySeriesCounts(ctx, studyUID)
	if err != nil {
		return studyCompletenessReport{}, fmt.Errorf("local series counts: %w", err)
	}
	return evaluateStudyCompleteness(expected, local), nil
}
