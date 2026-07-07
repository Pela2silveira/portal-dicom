package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type App struct {
	cfg                        Config
	db                         *sql.DB
	httpClient                 *http.Client
	orthancSearchClient        *http.Client
	logger                     *log.Logger
	loginRateLimiter           *InMemoryRateLimiter
	orthancModalityMu          sync.Mutex
	orthancModalities          map[string]string
	externalConfig             *ExternalConfig
	configLoadedAt             time.Time
	rbac                       *RBACPolicy
	usageRecorder              UsageRecorder
	identitySource             PatientIdentitySource
	professionalIdentitySource ProfessionalIdentitySource
	prestacionLookup           PrestacionLookupSource
	legacyHIS                  *LegacyHISClient
	patientSearchQueue         chan string
	retrieveQueue              chan string
	scheduledRetrieveQueue     chan string
	physicianAndesEnrichQueue  chan physicianAndesEnrichJob
	retrieveEventMu            sync.Mutex
	retrieveEventSubscribers   map[string]map[chan RetrieveJobEvent]struct{}
	systemEventMu              sync.Mutex
	systemEventSubscribers     map[chan SystemHealthEvent]struct{}
	systemHealthState          SystemHealthEvent
	systemHealthStateMu        sync.RWMutex
}

func (a *App) handleLivez(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "alive",
		"ts":     time.Now().UTC().Format(time.RFC3339),
	})
}

func (a *App) checkDB(ctx context.Context) bool {
	if a.db == nil {
		a.log("error", "db_unconfigured", map[string]any{})
		return false
	}
	if err := a.db.PingContext(ctx); err != nil {
		a.log("error", "db_ping_failed", map[string]any{"error": err.Error()})
		return false
	}
	return true
}

func (a *App) checkRemotePACS(parent context.Context, node PACSNodeConfig) bool {
	resolved := node.Resolved()
	switch resolved.HealthMode {
	case "dimse_c_echo":
		return a.checkRemotePACSViaOrthancEcho(parent, node, resolved)
	case "auth_qido":
		return a.checkRemotePACSWithAuthQIDO(parent, node, resolved)
	case "http", "mixed", "":
		return a.checkRemotePACSHTTP(parent, resolved)
	default:
		a.log("error", "remote_pacs_health_mode_unsupported", map[string]any{
			"node_id": node.ID,
			"mode":    resolved.HealthMode,
		})
		return false
	}
}

func (a *App) checkRemotePACSHTTP(parent context.Context, resolved PACSNodeResolvedConfig) bool {
	baseURL := strings.TrimRight(strings.TrimSpace(resolved.DICOMwebBaseURL), "/")
	if baseURL == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(parent, 1500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/studies?limit=1", nil)
	if err != nil {
		a.log("error", "remote_pacs_request_build_failed", map[string]any{
			"node_id": resolved.ID,
			"mode":    resolved.HealthMode,
			"error":   err.Error(),
		})
		return false
	}

	res, err := a.httpClient.Do(req)
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

func (a *App) sourceNodeAvailable(nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return true
	}
	event := a.currentSystemHealthEvent()
	return componentHealthy(event.Components, "remote_pacs:"+nodeID)
}

func (a *App) sourceNodeUsesHIS(nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return true
	}
	node, ok := a.configuredPACSNodeByID(nodeID)
	if !ok {
		return true
	}
	return node.HIS
}

func (a *App) withBrowserOriginCheck(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r != nil {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if !sameOriginRequest(r) {
					a.log("warn", "browser_origin_rejected", map[string]any{
						"method":      r.Method,
						"path":        r.URL.Path,
						"origin":      strings.TrimSpace(r.Header.Get("Origin")),
						"referer":     strings.TrimSpace(r.Referer()),
						"base_origin": requestBaseOrigin(r),
					})
					http.Error(w, "cross-site request rejected", http.StatusForbidden)
					return
				}
			}
		}
		next(w, r)
	}
}

func (a *App) portalSessionDuration() time.Duration {
	if a.externalConfig == nil || a.externalConfig.Portal.SessionTimeoutMinutes <= 0 {
		return 10 * time.Minute
	}
	return time.Duration(a.externalConfig.Portal.SessionTimeoutMinutes) * time.Minute
}

func (a *App) enforceLoginRateLimit(w http.ResponseWriter, r *http.Request, policy LoginRateLimitPolicy, identifier string) bool {
	if a == nil || a.loginRateLimiter == nil {
		return true
	}

	clientIP := clientIPForRateLimit(r)
	now := time.Now().UTC()
	longestRetryAfter := time.Duration(0)
	blockedScope := ""

	for _, rule := range policy.Rules {
		var key string
		switch rule.Scope {
		case "ip":
			if clientIP == "" {
				continue
			}
			key = policy.Endpoint + "|ip|" + clientIP
		case "identifier":
			normalizedIdentifier := strings.TrimSpace(identifier)
			if normalizedIdentifier == "" {
				continue
			}
			key = policy.Endpoint + "|identifier|" + normalizedIdentifier
		default:
			continue
		}

		allowed, retryAfter := a.loginRateLimiter.allow(key, rule.Limit, rule.Window, now)
		if allowed {
			continue
		}
		if retryAfter > longestRetryAfter {
			longestRetryAfter = retryAfter
			blockedScope = rule.Scope
		}
	}

	if blockedScope == "" {
		return true
	}

	retryAfterSeconds := int(math.Ceil(longestRetryAfter.Seconds()))
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"status":  "rate_limited",
		"message": "Se alcanzó el límite de intentos. Intente nuevamente en unos minutos.",
	})
	a.log("warn", "login_rate_limited", map[string]any{
		"endpoint":      policy.Endpoint,
		"scope":         blockedScope,
		"client_ip":     clientIP,
		"identifier":    identifier,
		"retry_after_s": retryAfterSeconds,
	})
	return false
}

func (a *App) logAccessionNumberProbe(scope, nodeID, studyUID, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	decoded, ok := decodeBase64IfPrintable(raw)
	a.log("info", "study_accession_probe", map[string]any{
		"scope":      scope,
		"node_id":    nodeID,
		"study_uid":  studyUID,
		"raw":        raw,
		"decoded":    decoded,
		"decoded_ok": ok,
	})
}

func (a *App) isStudyAvailableLocal(ctx context.Context, studyUID string) (bool, error) {
	ok, _, err := a.findOrthancStudy(ctx, studyUID)
	return ok, err
}

func (a *App) streamStudyArchiveByUID(ctx context.Context, w http.ResponseWriter, studyUID string) error {
	isLocal, orthancStudyID, err := a.findOrthancStudy(ctx, studyUID)
	if err != nil {
		return err
	}
	if !isLocal || strings.TrimSpace(orthancStudyID) == "" {
		return errors.New("study is not available in orthanc")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.OrthancURL, "/")+"/studies/"+url.PathEscape(orthancStudyID)+"/archive", nil)
	if err != nil {
		return err
	}
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("orthanc archive bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	filename := "study-" + sanitizeDownloadToken(studyUID) + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if res.Header.Get("Content-Length") != "" {
		w.Header().Set("Content-Length", res.Header.Get("Content-Length"))
	}
	_, err = io.Copy(w, res.Body)
	return err
}

func (a *App) listStudyPreviewItems(ctx context.Context, studyUID string, limit int) ([]PatientStudyPreviewItem, int, error) {
	if limit <= 0 {
		limit = 5
	}

	isLocal, orthancStudyID, err := a.findOrthancStudy(ctx, studyUID)
	if err != nil {
		return nil, 0, err
	}
	if !isLocal || strings.TrimSpace(orthancStudyID) == "" {
		return nil, 0, errors.New("study is not available in orthanc")
	}

	var study orthancStudyResource
	if err := a.getOrthancJSON(ctx, "/studies/"+url.PathEscape(orthancStudyID), &study); err != nil {
		return nil, 0, err
	}

	instanceIDs := make([]string, 0, limit)
	totalAvailable := 0
	for _, seriesID := range study.Series {
		var series orthancSeriesResource
		if err := a.getOrthancJSON(ctx, "/series/"+url.PathEscape(seriesID), &series); err != nil {
			return nil, 0, err
		}
		totalAvailable += len(series.Instances)
		for _, instanceID := range series.Instances {
			if len(instanceIDs) >= limit {
				continue
			}
			instanceIDs = append(instanceIDs, instanceID)
		}
	}

	items := make([]PatientStudyPreviewItem, 0, len(instanceIDs))
	for index, instanceID := range instanceIDs {
		imageDataURL, err := a.getOrthancPreviewDataURL(ctx, instanceID)
		if err != nil {
			continue
		}
		items = append(items, PatientStudyPreviewItem{
			InstanceID:   instanceID,
			ImageDataURL: imageDataURL,
			DownloadName: fmt.Sprintf("estudio-%s-imagen-%02d.jpg", sanitizeDownloadToken(studyUID), index+1),
		})
	}

	return items, totalAvailable, nil
}

func (a *App) getStudyOperationalState(ctx context.Context, studyUID string, fallbackCacheStatus, fallbackRetrieveStatus string) (string, string, string, int, string, string, error) {
	cacheStatus := fallbackCacheStatus
	retrieveStatus := fallbackRetrieveStatus
	retrievePhase := ""
	retrieveProgress := 0
	viewerURL := ""
	ohifViewerURL := ""

	var cachedOrthancStudyID string
	err := a.db.QueryRowContext(ctx, `
		SELECT COALESCE(cache_status, ''), COALESCE(orthanc_study_id, '')
		FROM cached_studies
		WHERE study_instance_uid = $1
	`, studyUID).Scan(&cacheStatus, &cachedOrthancStudyID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", "", 0, "", "", err
	}
	if cacheStatus == "" {
		cacheStatus = fallbackCacheStatus
	}

	var (
		latestRetrieveStatus   string
		latestRetrievePhase    string
		latestRetrieveProgress int
		retrieveCreatedAt      time.Time
		retrieveStartedAt      sql.NullTime
		retrieveFinishedAt     sql.NullTime
	)
	err = a.db.QueryRowContext(ctx, `
		SELECT status, COALESCE(phase, ''), COALESCE(progress, 0), created_at, started_at, finished_at
		FROM retrieve_jobs
		WHERE study_instance_uid = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, studyUID).Scan(&latestRetrieveStatus, &latestRetrievePhase, &latestRetrieveProgress, &retrieveCreatedAt, &retrieveStartedAt, &retrieveFinishedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", "", 0, "", "", err
	}
	if latestRetrieveStatus != "" {
		retrieveStatus = latestRetrieveStatus
		retrievePhase = latestRetrievePhase
		retrieveProgress = latestRetrieveProgress
		if (latestRetrieveStatus == "queued" || latestRetrieveStatus == "running") && !retrieveFinishedAt.Valid {
			lastActivity := retrieveCreatedAt
			if retrieveStartedAt.Valid {
				lastActivity = retrieveStartedAt.Time
			}
			if time.Since(lastActivity) > retrieveJobStaleAfter {
				retrieveStatus = "idle"
				retrievePhase = ""
				retrieveProgress = 0
			}
		}
	}

	isLocal, _, err := a.findOrthancStudy(ctx, studyUID)
	if err != nil {
		return "", "", "", 0, "", "", err
	}
	if isLocal {
		// A partial study is present and viewable; keep its partial flag rather
		// than masking it as complete so callers/UI can surface remediation.
		if cacheStatus != "local_partial" {
			cacheStatus = "local_complete"
		}
		retrieveStatus = "done"
		retrievePhase = "done"
		retrieveProgress = 100
		viewerURL = buildStoneViewerURL(studyUID)
		ohifViewerURL = buildOHIFViewerURL(studyUID)
	}

	return cacheStatus, retrieveStatus, retrievePhase, retrieveProgress, viewerURL, ohifViewerURL, nil
}

func (a *App) previewImageLimit() int {
	if a.externalConfig == nil || a.externalConfig.Portal.PreviewImageLimit <= 0 {
		return 5
	}
	return a.externalConfig.Portal.PreviewImageLimit
}

func (a *App) nodeDisplayName(nodeID string) string {
	nodeID = strings.TrimSpace(nodeID)
	for _, node := range a.externalConfig.PACSNodes {
		if node.ID == nodeID {
			name := strings.TrimSpace(node.Name)
			if name != "" {
				return name
			}
			break
		}
	}
	if nodeID == "" {
		return ""
	}
	return "Nodo remoto"
}

func (a *App) resolveLocationLabel(location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	for _, node := range a.externalConfig.PACSNodes {
		if strings.EqualFold(strings.TrimSpace(node.ID), location) || strings.EqualFold(strings.TrimSpace(node.Name), location) {
			name := strings.TrimSpace(node.Name)
			if name != "" {
				return name
			}
		}
	}
	if strings.EqualFold(location, "cache local") {
		return "Local"
	}
	return location
}

func (a *App) resolveLocationLabels(locations []string) []string {
	resolved := make([]string, 0, len(locations))
	for _, location := range locations {
		label := a.resolveLocationLabel(location)
		if label != "" {
			resolved = append(resolved, label)
		}
	}
	return mergeStringSets(resolved)
}

func (a *App) log(level, msg string, fields map[string]any) {
	payload := map[string]any{
		"level": level,
		"msg":   msg,
		"ts":    time.Now().UTC().Format(time.RFC3339),
	}

	for k, v := range fields {
		payload[k] = v
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		a.logger.Println(fmt.Sprintf(`{"level":"error","msg":"log_marshal_failed","error":%q}`, err.Error()))
		return
	}

	a.logger.Println(string(encoded))
}
