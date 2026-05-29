package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
)

type PersistedQIDOStudy struct {
	StudyInstanceUID  string
	SourceNodeID      string
	StudyDate         string
	PatientName       string
	PatientID         string
	StudyDescription  string
	NumberOfImages    int
	Modalities        []string
	Locations         []string
	AndesPrestacionID string
	AndesPrestacion   string
	AndesProfessional string
}

type qidoResponseItem map[string]dicomJSONAttribute

type PACSNodeSearchConfig struct {
	Mode            string         `json:"mode"`
	DICOMwebBaseURL string         `json:"dicomweb_base_url"`
	Auth            PACSAuthConfig `json:"auth"`
}

type CacheConfig struct {
	OrthancBaseURL string `json:"orthanc_base_url"`
	RetentionDays  int    `json:"retention_days"`
}

type PACSNodeSearchResponse struct {
	Mode            string           `json:"mode"`
	DICOMwebBaseURL string           `json:"dicomweb_base_url"`
	Auth            PACSAuthResponse `json:"auth"`
}

type SearchAdapter interface {
	SearchStudies(ctx context.Context, node PACSNodeResolvedConfig, query StudyQuery) ([]PhysicianResult, error)
}

type DICOMwebSearchAdapter struct{}
type DIMSESearchAdapter struct{}
type HybridSearchAdapter struct{}
type DIMSEHealthAdapter struct{}
type AuthQIDOHealthAdapter struct{}
type DIMSECechoHealthAdapter struct{}

func (a *App) checkRemotePACSWithAuthQIDO(parent context.Context, node PACSNodeConfig, resolved PACSNodeResolvedConfig) bool {
	baseURL := strings.TrimRight(strings.TrimSpace(resolved.DICOMwebBaseURL), "/")
	if baseURL == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()

	token, err := a.fetchPACSBearerToken(ctx, node)
	if err != nil {
		a.log("error", "remote_pacs_auth_failed", map[string]any{
			"node_id": resolved.ID,
			"mode":    resolved.HealthMode,
			"error":   err.Error(),
		})
		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/studies?limit=1", nil)
	if err != nil {
		a.log("error", "remote_pacs_request_build_failed", map[string]any{
			"node_id": resolved.ID,
			"mode":    resolved.HealthMode,
			"error":   err.Error(),
		})
		return false
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := a.orthancSearchClient.Do(req)
	if err != nil {
		a.log("error", "remote_pacs_unreachable", map[string]any{
			"node_id": resolved.ID,
			"mode":    resolved.HealthMode,
			"error":   err.Error(),
		})
		return false
	}
	defer res.Body.Close()

	ok := res.StatusCode >= 200 && res.StatusCode < 300
	if !ok {
		a.log("error", "remote_pacs_bad_status", map[string]any{
			"node_id":     resolved.ID,
			"mode":        resolved.HealthMode,
			"status_code": res.StatusCode,
		})
	}

	return ok
}

func (a *DICOMwebSearchAdapter) SearchStudies(_ context.Context, _ PACSNodeResolvedConfig, _ StudyQuery) ([]PhysicianResult, error) {
	return nil, errors.New("dicomweb search adapter not implemented")
}

func (a *DIMSESearchAdapter) SearchStudies(_ context.Context, _ PACSNodeResolvedConfig, _ StudyQuery) ([]PhysicianResult, error) {
	return nil, errors.New("dimse search adapter not implemented")
}

func (a *HybridSearchAdapter) SearchStudies(_ context.Context, _ PACSNodeResolvedConfig, _ StudyQuery) ([]PhysicianResult, error) {
	return nil, errors.New("hybrid search adapter not implemented")
}

func (a *DIMSEHealthAdapter) Check(_ context.Context, _ PACSNodeResolvedConfig) error {
	return errors.New("dimse health adapter not implemented")
}

func normalizeFuzzySearchText(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if upper == "" {
		return ""
	}

	replacer := strings.NewReplacer(
		"Á", "A",
		"À", "A",
		"Ä", "A",
		"Â", "A",
		"Ã", "A",
		"É", "E",
		"È", "E",
		"Ë", "E",
		"Ê", "E",
		"Í", "I",
		"Ì", "I",
		"Ï", "I",
		"Î", "I",
		"Ó", "O",
		"Ò", "O",
		"Ö", "O",
		"Ô", "O",
		"Õ", "O",
		"Ú", "U",
		"Ù", "U",
		"Ü", "U",
		"Û", "U",
		"Ñ", "N",
	)
	upper = replacer.Replace(upper)

	var b strings.Builder
	b.Grow(len(upper))
	lastWasSpace := true
	for _, r := range upper {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastWasSpace = false
			continue
		}
		if !lastWasSpace {
			b.WriteByte(' ')
			lastWasSpace = true
		}
	}

	return strings.TrimSpace(b.String())
}

func shouldRetryCFindWithoutDate(filters PhysicianSearchFilters) bool {
	return strings.TrimSpace(filters.DateFrom) != "" &&
		strings.TrimSpace(filters.PatientID) == "" &&
		strings.TrimSpace(filters.PatientName) == "" &&
		strings.TrimSpace(filters.Modality) == ""
}

func buildCFindStudyDate(dateFrom, dateTo string) string {
	from := strings.ReplaceAll(strings.TrimSpace(dateFrom), "-", "")
	to := strings.ReplaceAll(strings.TrimSpace(dateTo), "-", "")
	switch {
	case from != "" && to != "" && from == to:
		return from
	case from != "" && to != "":
		return from + "-" + to
	case from != "":
		return from
	case to != "":
		return to
	default:
		return ""
	}
}

func (a *App) upsertCachedStudy(ctx context.Context, studyUID, orthancStudyID string, locations []string, cacheStatus string) error {
	locationsJSON, err := json.Marshal(locations)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO cached_studies (
			study_instance_uid, orthanc_study_id, first_seen_at, last_verified_at, cache_status, locations_json
		) VALUES (
			$1, $2, now(), now(), $3, $4::jsonb
		)
		ON CONFLICT (study_instance_uid) DO UPDATE SET
			orthanc_study_id = EXCLUDED.orthanc_study_id,
			last_verified_at = now(),
			cache_status = EXCLUDED.cache_status,
			locations_json = EXCLUDED.locations_json
	`, studyUID, orthancStudyID, cacheStatus, string(locationsJSON))
	return err
}

func buildQIDODateRange(dateFrom, dateTo string) string {
	from := strings.ReplaceAll(strings.TrimSpace(dateFrom), "-", "")
	to := strings.ReplaceAll(strings.TrimSpace(dateTo), "-", "")
	switch {
	case from != "" && to != "":
		return from + "-" + to
	case from != "":
		return from + "-"
	case to != "":
		return "-" + to
	default:
		return ""
	}
}

func (a *App) cachedStudyLocations(ctx context.Context, studyUID string) ([]string, error) {
	var raw []byte
	err := a.db.QueryRowContext(ctx, `
		SELECT locations_json
		FROM cached_studies
		WHERE study_instance_uid = $1
	`, studyUID).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	var locations []string
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &locations); err != nil {
		return nil, err
	}
	return a.resolveLocationLabels(locations), nil
}

func (a *App) loadPersistedQIDOStudies(ctx context.Context, sourceNodeID string, studyUIDs []string) (map[string]PersistedQIDOStudy, error) {
	sourceNodeID = strings.TrimSpace(sourceNodeID)
	if sourceNodeID == "" || len(studyUIDs) == 0 {
		return map[string]PersistedQIDOStudy{}, nil
	}

	rows, err := a.db.QueryContext(ctx, `
		SELECT
			study_instance_uid,
			COALESCE(study_date, ''),
			COALESCE(patient_name, ''),
			COALESCE(patient_id, ''),
			COALESCE(study_description, ''),
			modalities_json,
			locations_json,
			COALESCE(andes_prestacion_id, ''),
			COALESCE(andes_prestacion, ''),
			COALESCE(andes_professional, '')
		FROM qido_study_cache
		WHERE source_node_id = $1
		  AND study_instance_uid = ANY($2)
	`, sourceNodeID, studyUIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make(map[string]PersistedQIDOStudy, len(studyUIDs))
	for rows.Next() {
		var (
			study         PersistedQIDOStudy
			modalitiesRaw []byte
			locationsRaw  []byte
		)
		if err := rows.Scan(
			&study.StudyInstanceUID,
			&study.StudyDate,
			&study.PatientName,
			&study.PatientID,
			&study.StudyDescription,
			&modalitiesRaw,
			&locationsRaw,
			&study.AndesPrestacionID,
			&study.AndesPrestacion,
			&study.AndesProfessional,
		); err != nil {
			return nil, err
		}
		study.SourceNodeID = sourceNodeID
		if len(modalitiesRaw) > 0 {
			_ = json.Unmarshal(modalitiesRaw, &study.Modalities)
		}
		if len(locationsRaw) > 0 {
			_ = json.Unmarshal(locationsRaw, &study.Locations)
		}
		results[study.StudyInstanceUID] = study
	}

	return results, rows.Err()
}

func (a *App) persistQIDOStudies(ctx context.Context, studies []PersistedQIDOStudy) error {
	for _, study := range studies {
		if strings.TrimSpace(study.StudyInstanceUID) == "" || strings.TrimSpace(study.SourceNodeID) == "" {
			continue
		}

		modalitiesJSON, err := json.Marshal(study.Modalities)
		if err != nil {
			return fmt.Errorf("marshal qido study modalities: %w", err)
		}
		locationsJSON, err := json.Marshal(study.Locations)
		if err != nil {
			return fmt.Errorf("marshal qido study locations: %w", err)
		}

		if _, err := a.db.ExecContext(ctx, `
			INSERT INTO qido_study_cache (
				study_instance_uid, source_node_id, study_date, patient_name, patient_id,
				study_description, modalities_json, locations_json,
				andes_prestacion_id, andes_prestacion, andes_professional,
				first_seen_at, last_seen_at, last_andes_enriched_at
			) VALUES (
				$1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''),
				NULLIF($6, ''), $7::jsonb, $8::jsonb,
				NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''),
				now(), now(),
				CASE
					WHEN NULLIF($9, '') IS NOT NULL OR NULLIF($10, '') IS NOT NULL OR NULLIF($11, '') IS NOT NULL THEN now()
					ELSE NULL
				END
			)
			ON CONFLICT (study_instance_uid, source_node_id) DO UPDATE SET
				study_date = EXCLUDED.study_date,
				patient_name = EXCLUDED.patient_name,
				patient_id = EXCLUDED.patient_id,
				study_description = EXCLUDED.study_description,
				modalities_json = EXCLUDED.modalities_json,
				locations_json = EXCLUDED.locations_json,
				andes_prestacion_id = COALESCE(EXCLUDED.andes_prestacion_id, qido_study_cache.andes_prestacion_id),
				andes_prestacion = COALESCE(EXCLUDED.andes_prestacion, qido_study_cache.andes_prestacion),
				andes_professional = COALESCE(EXCLUDED.andes_professional, qido_study_cache.andes_professional),
				last_seen_at = now(),
				last_andes_enriched_at = CASE
					WHEN EXCLUDED.andes_prestacion_id IS NOT NULL OR EXCLUDED.andes_prestacion IS NOT NULL OR EXCLUDED.andes_professional IS NOT NULL
						THEN now()
					ELSE qido_study_cache.last_andes_enriched_at
				END
		`,
			study.StudyInstanceUID,
			study.SourceNodeID,
			study.StudyDate,
			study.PatientName,
			study.PatientID,
			study.StudyDescription,
			string(modalitiesJSON),
			string(locationsJSON),
			nullIfBlank(study.AndesPrestacionID),
			nullIfBlank(study.AndesPrestacion),
			nullIfBlank(study.AndesProfessional),
		); err != nil {
			return fmt.Errorf("upsert qido study cache %s/%s: %w", study.SourceNodeID, study.StudyInstanceUID, err)
		}
	}

	return nil
}
