package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const studyCacheDeleteTimeout = 2 * time.Minute

type studyCacheDeleteResponse struct {
	StudyInstanceUID   string `json:"study_instance_uid"`
	OrthancStudyID     string `json:"orthanc_study_id,omitempty"`
	DeletedFromOrthanc bool   `json:"deleted_from_orthanc"`
	CacheStatus        string `json:"cache_status"`
}

// physicianCanDeleteCache reports whether the physician's resolved RBAC role
// grants study:cache_delete. Only the operator role (wildcard) has it by default.
func (a *App) physicianCanDeleteCache(summary PhysicianSummary) bool {
	if a == nil || a.rbac == nil {
		return false
	}
	act, ok := actionCatalog[ActionStudyCacheDelete]
	if !ok || act.Permission == "" {
		return false
	}
	return a.rbac.hasPermission(a.rbac.roleForPhysician(summary), act.Permission)
}

// orthancDeleteAccepted treats a missing study as already removed so a retry
// after a partial failure can finish the portal-side reset.
func orthancDeleteAccepted(status int) bool {
	return status == http.StatusNotFound || (status >= 200 && status < 300)
}

// retrieveJobInFlight reports whether a retrieve job should block a cache
// delete. A finished job, any status other than queued/running, or a job that
// has been quiet longer than retrieveJobStaleAfter does not block.
func retrieveJobInFlight(status string, createdAt, startedAt time.Time, hasStarted, finished bool, now time.Time) bool {
	if finished || (status != "queued" && status != "running") {
		return false
	}
	last := createdAt
	if hasStarted {
		last = startedAt
	}
	if last.IsZero() {
		return true
	}
	return now.Sub(last) <= retrieveJobStaleAfter
}

func (a *App) handlePhysicianStudyCacheDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	studyUID := studyUIDFromActionPath(r.URL.Path, "/api/physician/studies/", "/cache")
	if studyUID == "" {
		http.NotFound(w, r)
		return
	}
	setActionDim(r.Context(), "study_uid", studyUID)

	ctx, cancel := context.WithTimeout(r.Context(), studyCacheDeleteTimeout)
	defer cancel()

	inFlight, err := a.studyRetrieveInFlight(ctx, studyUID)
	if err != nil {
		a.log("error", "study_cache_delete_inflight_check_failed", map[string]any{
			"study_instance_uid": studyUID,
			"error":              err.Error(),
		})
		http.Error(w, "No se pudo quitar el estudio de la caché local.", http.StatusInternalServerError)
		return
	}
	if inFlight {
		http.Error(w, "Hay una recuperación en curso para este estudio.", http.StatusConflict)
		return
	}

	present, orthancID, err := a.findOrthancStudy(ctx, studyUID)
	if err != nil {
		a.log("error", "study_cache_delete_lookup_failed", map[string]any{
			"study_instance_uid": studyUID,
			"error":              err.Error(),
		})
		http.Error(w, "No se pudo quitar el estudio de la caché local.", http.StatusBadGateway)
		return
	}

	deletedFromOrthanc := false
	if present {
		if err := a.deleteOrthancStudy(ctx, orthancID); err != nil {
			a.log("error", "study_cache_delete_orthanc_failed", map[string]any{
				"study_instance_uid": studyUID,
				"orthanc_study_id":   orthancID,
				"error":              err.Error(),
			})
			http.Error(w, "No se pudo quitar el estudio de la caché local.", http.StatusBadGateway)
			return
		}
		deletedFromOrthanc = true
		setActionDim(r.Context(), "orthanc_study_id", orthancID)
	}

	if err := a.clearPortalStudyCache(ctx, studyUID); err != nil {
		a.log("error", "study_cache_delete_db_failed", map[string]any{
			"study_instance_uid": studyUID,
			"orthanc_study_id":   orthancID,
			"error":              err.Error(),
		})
		http.Error(w, "No se pudo actualizar el estado local del estudio.", http.StatusInternalServerError)
		return
	}

	a.log("info", "study_cache_deleted", map[string]any{
		"study_instance_uid":   studyUID,
		"orthanc_study_id":     orthancID,
		"deleted_from_orthanc": deletedFromOrthanc,
	})

	writeJSON(w, http.StatusOK, studyCacheDeleteResponse{
		StudyInstanceUID:   studyUID,
		OrthancStudyID:     orthancID,
		DeletedFromOrthanc: deletedFromOrthanc,
		CacheStatus:        cacheStatusNotLocal,
	})
}

func (a *App) studyRetrieveInFlight(ctx context.Context, studyUID string) (bool, error) {
	if a == nil || a.db == nil {
		return false, errors.New("database unavailable")
	}
	var (
		status     string
		createdAt  time.Time
		startedAt  sql.NullTime
		finishedAt sql.NullTime
	)
	err := a.db.QueryRowContext(ctx, `
		SELECT status, created_at, started_at, finished_at
		FROM retrieve_jobs
		WHERE study_instance_uid = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, studyUID).Scan(&status, &createdAt, &startedAt, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return retrieveJobInFlight(status, createdAt, startedAt.Time, startedAt.Valid, finishedAt.Valid, time.Now()), nil
}

func (a *App) deleteOrthancStudy(ctx context.Context, orthancStudyID string) error {
	orthancStudyID = strings.TrimSpace(orthancStudyID)
	if orthancStudyID == "" {
		return errors.New("missing orthanc study id")
	}
	if strings.TrimSpace(a.cfg.OrthancURL) == "" {
		return errors.New("orthanc url is not configured")
	}

	endpoint := strings.TrimRight(a.cfg.OrthancURL, "/") + "/studies/" + url.PathEscape(orthancStudyID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build orthanc study delete request: %w", err)
	}
	a.applyOrthancInternalRequestAuth(req)

	client := a.orthancDeleteClient()
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute orthanc study delete request: %w", err)
	}
	defer res.Body.Close()
	if orthancDeleteAccepted(res.StatusCode) {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return fmt.Errorf("orthanc study delete bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
}

// orthancDeleteClient reuses the shared HTTP client but allows a study delete
// to run longer than the 15s default. Cloning keeps the test transport.
func (a *App) orthancDeleteClient() *http.Client {
	if a == nil || a.httpClient == nil {
		return &http.Client{Timeout: studyCacheDeleteTimeout}
	}
	if a.httpClient.Timeout == 0 || a.httpClient.Timeout >= studyCacheDeleteTimeout {
		return a.httpClient
	}
	cloned := *a.httpClient
	cloned.Timeout = studyCacheDeleteTimeout
	return &cloned
}

// clearPortalStudyCache drops the local-cache projection for one study.
// It does not delete qido_study_cache or retrieve history: the next remote
// search rewrites the metadata, and the audit of past retrieves stays.
func (a *App) clearPortalStudyCache(ctx context.Context, studyUID string) error {
	if a == nil || a.db == nil {
		return errors.New("database unavailable")
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
		UPDATE cached_studies
		SET cache_status = 'not_local',
		    orthanc_study_id = NULL,
		    last_verified_at = now(),
		    expected_series_count = NULL,
		    present_series_count = NULL,
		    missing_series_json = '[]'::jsonb,
		    last_completeness_checked_at = NULL
		WHERE study_instance_uid = $1
	`, studyUID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE patient_study_access
		SET availability_status = 'pending_retrieve',
		    local_orthanc_study_id = NULL,
		    last_seen_at = now()
		WHERE study_instance_uid = $1
		  AND availability_status = 'available_local'
	`, studyUID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE study_share_links
		SET status = 'revoked',
		    revoked_at = now()
		WHERE study_instance_uid = $1
		  AND status = 'active'
		  AND revoked_at IS NULL
	`, studyUID); err != nil {
		return err
	}

	return tx.Commit()
}
