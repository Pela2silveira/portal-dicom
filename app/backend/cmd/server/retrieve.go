package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RetrieveJobEvent struct {
	JobID            string `json:"job_id"`
	StudyInstanceUID string `json:"study_instance_uid"`
	Status           string `json:"status"`
	Phase            string `json:"phase,omitempty"`
	Progress         int    `json:"progress,omitempty"`
	Error            string `json:"error,omitempty"`
}

type retrieveJobSnapshot struct {
	JobID            string
	StudyInstanceUID string
	Status           string
	Phase            string
	Progress         int
	Error            string
}

type PACSNodeRetrieveConfig struct {
	Mode          string `json:"mode"`
	AET           string `json:"aet"`
	DICOMHost     string `json:"dicom_host"`
	DICOMPort     int    `json:"dicom_port"`
	SupportsCMove bool   `json:"supports_cmove"`
	SupportsCGet  bool   `json:"supports_cget"`
}

type PACSNodeRetrieveResponse struct {
	Mode          string `json:"mode"`
	AET           string `json:"aet"`
	DICOMHost     string `json:"dicom_host"`
	DICOMPort     int    `json:"dicom_port"`
	SupportsCMove bool   `json:"supports_cmove"`
	SupportsCGet  bool   `json:"supports_cget"`
}

type RetrieveAdapter interface {
	RetrieveStudy(ctx context.Context, node PACSNodeResolvedConfig, studyInstanceUID string) error
}
type DICOMwebRetrieveAdapter struct{}
type DIMSERetrieveAdapter struct{}
type HybridRetrieveAdapter struct{}

func (a *App) startRetrieveWorker() {
	workers := a.retrieveWorkerConcurrency()
	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		go func() {
			for {
				select {
				case jobID := <-a.retrieveQueue:
					a.processRetrieveJob(jobID)
					continue
				default:
				}

				select {
				case jobID := <-a.retrieveQueue:
					a.processRetrieveJob(jobID)
				case jobID := <-a.scheduledRetrieveQueue:
					a.processRetrieveJob(jobID)
				}
			}
		}()
	}
}

func (a *App) startScheduledRetrieveWorker() {
	if a.externalConfig == nil || !a.externalConfig.Portal.ScheduledRetrieveEnabled {
		return
	}

	go func() {
		ticker := time.NewTicker(a.scheduledRetrieveInterval())
		defer ticker.Stop()

		for range ticker.C {
			a.runScheduledRetrieveCycle()
		}
	}()
}

func (a *App) enqueueRetrieveJob(jobID string) {
	a.retrieveQueue <- jobID
}

func (a *App) enqueueScheduledRetrieveJob(jobID string) {
	select {
	case a.scheduledRetrieveQueue <- jobID:
	default:
		a.log("error", "scheduled_retrieve_queue_full", map[string]any{
			"job_id": jobID,
		})
	}
}

func (a *App) handleRetrieveJobEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	const prefix = "/api/retrieve/jobs/"
	if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, "/events") {
		http.NotFound(w, r)
		return
	}

	jobID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), "/events")
	jobID = strings.Trim(jobID, "/")
	if jobID == "" {
		http.NotFound(w, r)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	initialEvent, err := a.getRetrieveJobEvent(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load retrieve job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if err := writeRetrieveSSEEvent(w, "status", initialEvent); err != nil {
		return
	}
	flusher.Flush()
	if initialEvent.Status == "done" || initialEvent.Status == "failed" {
		return
	}

	subscriber := a.subscribeRetrieveJob(jobID)
	defer a.unsubscribeRetrieveJob(jobID, subscriber)

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-subscriber:
			if err := writeRetrieveSSEEvent(w, "status", event); err != nil {
				return
			}
			flusher.Flush()
			if event.Status == "done" || event.Status == "failed" {
				return
			}
		}
	}
}

func (a *DICOMwebRetrieveAdapter) RetrieveStudy(_ context.Context, _ PACSNodeResolvedConfig, _ string) error {
	return errors.New("dicomweb retrieve adapter not implemented")
}

func (a *DIMSERetrieveAdapter) RetrieveStudy(_ context.Context, _ PACSNodeResolvedConfig, _ string) error {
	return errors.New("dimse retrieve adapter not implemented")
}

func (a *HybridRetrieveAdapter) RetrieveStudy(_ context.Context, _ PACSNodeResolvedConfig, _ string) error {
	return errors.New("hybrid retrieve adapter not implemented")
}

func (a *App) processRetrieveJob(jobID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var (
		studyInstanceUID string
		sourceNodeCode   string
		actorType        string
		actorID          string
		status           string
	)
	err := a.db.QueryRowContext(ctx, `
		SELECT
			rj.study_instance_uid,
			COALESCE(pn.code, ''),
			COALESCE(rj.requested_by_actor_type, ''),
			COALESCE(rj.requested_by_actor_id::text, ''),
			rj.status
		FROM retrieve_jobs rj
		LEFT JOIN pacs_nodes pn ON pn.id = rj.source_node_id
		WHERE rj.id = $1::uuid
	`, jobID).Scan(&studyInstanceUID, &sourceNodeCode, &actorType, &actorID, &status)
	if err != nil {
		a.log("error", "retrieve_job_load_failed", map[string]any{
			"job_id": jobID,
			"error":  err.Error(),
		})
		return
	}
	if status == "done" {
		return
	}

	if err := a.updateRetrieveJobStatus(ctx, jobID, "running", "preparing", 0, "", "", "", 0, true); err != nil {
		a.log("error", "retrieve_job_mark_running_failed", map[string]any{
			"job_id": jobID,
			"error":  err.Error(),
		})
		return
	}

	node, err := a.getConfiguredNode(sourceNodeCode)
	if err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", "preparing", 0, err.Error(), "", "", 0, false)
		a.log("error", "retrieve_job_node_resolve_failed", map[string]any{
			"job_id":         jobID,
			"source_node_id": sourceNodeCode,
			"error":          err.Error(),
		})
		return
	}

	startedAt := time.Now()
	a.log("info", "retrieve_job_started", map[string]any{
		"job_id":             jobID,
		"study_instance_uid": studyInstanceUID,
		"source_node_id":     sourceNodeCode,
		"actor_type":         actorType,
		"actor_id":           actorID,
	})

	if err := a.ensureOrthancModality(ctx, node); err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", "preparing", 0, err.Error(), "", "", 0, false)
		return
	}

	orthancJobID, err := a.startOrthancCGet(ctx, node, studyInstanceUID)
	if err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", "preparing", 0, err.Error(), "", "", 0, false)
		return
	}
	if err := a.updateRetrieveJobStatus(ctx, jobID, "running", "retrieving", 0, "", orthancJobID, "", 0, false); err != nil {
		a.log("error", "retrieve_job_save_orthanc_job_failed", map[string]any{
			"job_id":         jobID,
			"orthanc_job_id": orthancJobID,
			"error":          err.Error(),
		})
	}

	orthancStudyID, err := a.monitorOrthancRetrieveJob(ctx, jobID, orthancJobID, studyInstanceUID)
	if err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", "retrieving", 0, err.Error(), orthancJobID, "", 0, false)
		return
	}

	if err := a.updateRetrieveJobStatus(ctx, jobID, "running", "verifying", 100, "", orthancJobID, orthancStudyID, 0, false); err != nil {
		a.log("error", "retrieve_job_mark_verifying_failed", map[string]any{
			"job_id": jobID,
			"error":  err.Error(),
		})
	}

	// Completeness is best-effort: a lookup failure must not fail an otherwise
	// successful retrieve, it only means we cannot assert local_partial.
	report, cerr := a.verifyRetrievedStudyCompleteness(ctx, node, studyInstanceUID)
	if cerr != nil {
		a.log("warn", "retrieve_completeness_check_failed", map[string]any{
			"job_id":             jobID,
			"study_instance_uid": studyInstanceUID,
			"source_node_id":     sourceNodeCode,
			"error":              cerr.Error(),
		})
		report = studyCompletenessReport{}
	}
	if report.Evaluated && !report.Complete {
		a.log("warn", "retrieve_study_incomplete", map[string]any{
			"job_id":             jobID,
			"study_instance_uid": studyInstanceUID,
			"source_node_id":     sourceNodeCode,
			"expected_series":    report.ExpectedSeries,
			"present_series":     report.PresentSeries,
			"missing_series":     len(report.MissingSeries),
		})
	}

	if err := a.completeRetrieveSuccess(ctx, jobID, studyInstanceUID, orthancStudyID, sourceNodeCode, report); err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", "verifying", 100, err.Error(), orthancJobID, orthancStudyID, 0, false)
		a.log("error", "retrieve_job_mark_done_failed", map[string]any{
			"job_id": jobID,
			"error":  err.Error(),
		})
		return
	}

	a.log("info", "retrieve_job_completed", map[string]any{
		"job_id":             jobID,
		"study_instance_uid": studyInstanceUID,
		"source_node_id":     sourceNodeCode,
		"actor_type":         actorType,
		"actor_id":           actorID,
		"orthanc_study_id":   orthancStudyID,
		"duration_ms":        time.Since(startedAt).Milliseconds(),
	})
}

func (a *App) findActiveRetrieveJob(ctx context.Context, studyUID, actorType, actorID string) (*retrieveJobSnapshot, error) {
	var snapshot retrieveJobSnapshot
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, study_instance_uid, status, COALESCE(phase, ''), COALESCE(progress, 0), COALESCE(error, '')
		FROM retrieve_jobs
		WHERE study_instance_uid = $1
		  AND requested_by_actor_type = $2
		  AND requested_by_actor_id = $3::uuid
		  AND status IN ('queued', 'running')
		  AND COALESCE(started_at, created_at) >= now() - interval '10 minutes'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, studyUID, actorType, actorID).Scan(&snapshot.JobID, &snapshot.StudyInstanceUID, &snapshot.Status, &snapshot.Phase, &snapshot.Progress, &snapshot.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active retrieve job: %w", err)
	}
	return &snapshot, nil
}

func (a *App) findActiveRetrieveJobByStudy(ctx context.Context, studyUID string) (*retrieveJobSnapshot, error) {
	var snapshot retrieveJobSnapshot
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, study_instance_uid, status, COALESCE(phase, ''), COALESCE(progress, 0), COALESCE(error, '')
		FROM retrieve_jobs
		WHERE study_instance_uid = $1
		  AND status IN ('queued', 'running')
		  AND COALESCE(started_at, created_at) >= now() - interval '10 minutes'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, studyUID).Scan(&snapshot.JobID, &snapshot.StudyInstanceUID, &snapshot.Status, &snapshot.Phase, &snapshot.Progress, &snapshot.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active retrieve job by study: %w", err)
	}
	return &snapshot, nil
}

func (a *App) getRetrieveJobEvent(ctx context.Context, jobID string) (RetrieveJobEvent, error) {
	var event RetrieveJobEvent
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, study_instance_uid, status, COALESCE(phase, ''), COALESCE(progress, 0), COALESCE(error, '')
		FROM retrieve_jobs
		WHERE id = $1::uuid
	`, jobID).Scan(&event.JobID, &event.StudyInstanceUID, &event.Status, &event.Phase, &event.Progress, &event.Error)
	return event, err
}

func (a *App) subscribeRetrieveJob(jobID string) chan RetrieveJobEvent {
	a.retrieveEventMu.Lock()
	defer a.retrieveEventMu.Unlock()

	ch := make(chan RetrieveJobEvent, 4)
	if a.retrieveEventSubscribers[jobID] == nil {
		a.retrieveEventSubscribers[jobID] = make(map[chan RetrieveJobEvent]struct{})
	}
	a.retrieveEventSubscribers[jobID][ch] = struct{}{}
	return ch
}

func (a *App) unsubscribeRetrieveJob(jobID string, ch chan RetrieveJobEvent) {
	a.retrieveEventMu.Lock()
	defer a.retrieveEventMu.Unlock()

	subscribers := a.retrieveEventSubscribers[jobID]
	if subscribers == nil {
		close(ch)
		return
	}
	delete(subscribers, ch)
	if len(subscribers) == 0 {
		delete(a.retrieveEventSubscribers, jobID)
	}
	close(ch)
}

func (a *App) publishRetrieveJobEvent(event RetrieveJobEvent) {
	a.retrieveEventMu.Lock()
	defer a.retrieveEventMu.Unlock()

	for subscriber := range a.retrieveEventSubscribers[event.JobID] {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func writeRetrieveSSEEvent(w io.Writer, eventName string, event RetrieveJobEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

func (a *App) retrieveBlockedModalities() map[string]struct{} {
	out := map[string]struct{}{}
	configured := []string{"KO"}
	if a.externalConfig != nil && len(a.externalConfig.Portal.RetrieveBlockedModalities) > 0 {
		configured = a.externalConfig.Portal.RetrieveBlockedModalities
	}
	for _, modality := range configured {
		normalized := strings.ToUpper(strings.TrimSpace(modality))
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	return out
}

func (a *App) blockedRetrieveModality(modalities []string) string {
	blocked := a.retrieveBlockedModalities()
	for _, modality := range modalities {
		normalized := strings.ToUpper(strings.TrimSpace(modality))
		if normalized == "" {
			continue
		}
		if _, found := blocked[normalized]; found {
			return normalized
		}
	}
	return ""
}

func (a *App) insertRetrieveJob(ctx context.Context, studyUID, sourceNodeID, actorType, actorID string) (string, error) {
	var jobID string
	err := a.db.QueryRowContext(ctx, `
		INSERT INTO retrieve_jobs (
			study_instance_uid, source_node_id, requested_by_actor_type, requested_by_actor_id, status
		) VALUES (
			$1, (SELECT id FROM pacs_nodes WHERE code = $2), NULLIF($3, ''), CASE WHEN $4 = '' THEN NULL ELSE $4::uuid END, 'queued'
		)
		RETURNING id::text
	`, studyUID, sourceNodeID, actorType, actorID).Scan(&jobID)
	return jobID, err
}

func (a *App) updateRetrieveJobStatus(ctx context.Context, jobID, status, phase string, progress int, errMsg, orthancJobID, orthancStudyID string, instancesReceived int, setStarted bool) error {
	query := `
		UPDATE retrieve_jobs
		SET status = $2,
		    phase = NULLIF($3, ''),
		    progress = GREATEST(0, LEAST($4, 100)),
		    error = NULLIF($5, ''),
		    orthanc_job_id = NULLIF($6, ''),
		    orthanc_study_id = NULLIF($7, ''),
		    instances_received = NULLIF($8, 0),
		    finished_at = CASE WHEN $2 IN ('done', 'failed') THEN now() ELSE finished_at END
	`
	args := []any{jobID, status, phase, progress, errMsg, orthancJobID, orthancStudyID, instancesReceived}
	if setStarted {
		query += `, started_at = now()`
	}
	query += ` WHERE id = $1::uuid`
	if _, err := a.db.ExecContext(ctx, query, args...); err != nil {
		return err
	}

	event, err := a.getRetrieveJobEvent(ctx, jobID)
	if err == nil {
		a.publishRetrieveJobEvent(event)
	}
	return nil
}

func (a *App) retrieveProgressPollInterval() time.Duration {
	if a.externalConfig == nil || a.externalConfig.Portal.RetrieveProgressPollSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(a.externalConfig.Portal.RetrieveProgressPollSeconds) * time.Second
}

func (a *App) retrieveWorkerConcurrency() int {
	if a.externalConfig == nil || a.externalConfig.Portal.RetrieveWorkerConcurrency <= 0 {
		return 2
	}
	return a.externalConfig.Portal.RetrieveWorkerConcurrency
}

func (a *App) scheduledRetrieveInterval() time.Duration {
	if a.externalConfig == nil || a.externalConfig.Portal.ScheduledRetrieveIntervalMinutes <= 0 {
		return time.Hour
	}
	return time.Duration(a.externalConfig.Portal.ScheduledRetrieveIntervalMinutes) * time.Minute
}

func (a *App) scheduledRetrieveMaxStudyAgeDays() int {
	if a.externalConfig == nil || a.externalConfig.Portal.ScheduledRetrieveMaxStudyAgeDays <= 0 {
		return 7
	}
	return a.externalConfig.Portal.ScheduledRetrieveMaxStudyAgeDays
}

func (a *App) scheduledRetrieveBatchSize() int {
	if a.externalConfig == nil || a.externalConfig.Portal.ScheduledRetrieveBatchSize <= 0 {
		return 5
	}
	return a.externalConfig.Portal.ScheduledRetrieveBatchSize
}

type scheduledRetrieveCandidate struct {
	StudyInstanceUID string
	SourceNodeID     string
}

func (a *App) runScheduledRetrieveCycle() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	candidates, err := a.listScheduledRetrieveCandidates(ctx, a.scheduledRetrieveMaxStudyAgeDays(), a.scheduledRetrieveBatchSize())
	if err != nil {
		a.log("error", "scheduled_retrieve_candidates_failed", map[string]any{
			"error": err.Error(),
		})
		return
	}
	if len(candidates) == 0 {
		return
	}

	enqueued := 0
	for _, candidate := range candidates {
		if !a.sourceNodeAvailable(candidate.SourceNodeID) {
			continue
		}
		activeJob, err := a.findActiveRetrieveJobByStudy(ctx, candidate.StudyInstanceUID)
		if err != nil || activeJob != nil {
			continue
		}

		jobID, err := a.insertRetrieveJob(ctx, candidate.StudyInstanceUID, candidate.SourceNodeID, "system", "")
		if err != nil {
			a.log("error", "scheduled_retrieve_enqueue_failed", map[string]any{
				"study_instance_uid": candidate.StudyInstanceUID,
				"source_node_id":     candidate.SourceNodeID,
				"error":              err.Error(),
			})
			continue
		}
		a.enqueueScheduledRetrieveJob(jobID)
		enqueued++
	}

	if enqueued > 0 {
		a.log("info", "scheduled_retrieve_cycle_completed", map[string]any{
			"candidates": len(candidates),
			"enqueued":   enqueued,
		})
	}
}

func (a *App) listScheduledRetrieveCandidates(ctx context.Context, maxStudyAgeDays, batchSize int) ([]scheduledRetrieveCandidate, error) {
	rows, err := a.db.QueryContext(ctx, `
		WITH ranked AS (
			SELECT
				q.study_instance_uid,
				q.source_node_id,
				q.last_seen_at,
				ROW_NUMBER() OVER (
					PARTITION BY q.study_instance_uid
					ORDER BY q.last_seen_at DESC, q.source_node_id ASC
				) AS rn
			FROM qido_study_cache q
			LEFT JOIN cached_studies cs ON cs.study_instance_uid = q.study_instance_uid
			WHERE COALESCE(cs.cache_status, 'not_local') <> 'local_complete'
			  AND COALESCE(q.source_node_id, '') <> ''
			  AND COALESCE(NULLIF(REPLACE(q.study_date, '-', ''), ''), TO_CHAR(q.last_seen_at, 'YYYYMMDD')) >= TO_CHAR(CURRENT_DATE - ($1::int || ' days')::interval, 'YYYYMMDD')
		)
		SELECT study_instance_uid, source_node_id
		FROM ranked
		WHERE rn = 1
		ORDER BY last_seen_at DESC, study_instance_uid ASC
		LIMIT $2
	`, maxStudyAgeDays, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]scheduledRetrieveCandidate, 0, batchSize)
	for rows.Next() {
		var candidate scheduledRetrieveCandidate
		if err := rows.Scan(&candidate.StudyInstanceUID, &candidate.SourceNodeID); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}

	return candidates, rows.Err()
}

func (a *App) completeRetrieveSuccess(ctx context.Context, jobID, studyUID, orthancStudyID, sourceNodeID string, report studyCompletenessReport) error {
	// A partial study is still viewable (the present series open fine); we only
	// stop asserting local_complete so the UI and background remediation can act.
	cacheStatus := "local_complete"
	if report.Evaluated && !report.Complete {
		cacheStatus = "local_partial"
	}

	missingSeriesJSON, err := json.Marshal(report.MissingSeries)
	if err != nil {
		return err
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
		UPDATE patient_study_access
		SET availability_status = 'available_local',
		    local_orthanc_study_id = $2,
		    last_seen_at = now(),
		    last_authorized_at = now(),
		    source_json = jsonb_set(
		      jsonb_set(COALESCE(source_json, '{}'::jsonb), '{source_node_id}', to_jsonb($3::text), true),
		      '{orthanc_study_id}', to_jsonb($2::text), true
		    )
		WHERE study_instance_uid = $1
	`, studyUID, orthancStudyID, sourceNodeID); err != nil {
		return err
	}

	locationsJSON, err := json.Marshal([]string{sourceNodeID})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO cached_studies (
			study_instance_uid, orthanc_study_id, first_seen_at, last_verified_at, cache_status, locations_json,
			expected_series_count, present_series_count, missing_series_json, last_completeness_checked_at
		) VALUES (
			$1, $2, now(), now(), $4, $3::jsonb,
			$5, $6, $7::jsonb, CASE WHEN $8 THEN now() ELSE NULL END
		)
		ON CONFLICT (study_instance_uid) DO UPDATE SET
			orthanc_study_id = EXCLUDED.orthanc_study_id,
			last_verified_at = now(),
			cache_status = EXCLUDED.cache_status,
			locations_json = EXCLUDED.locations_json,
			expected_series_count = EXCLUDED.expected_series_count,
			present_series_count = EXCLUDED.present_series_count,
			missing_series_json = EXCLUDED.missing_series_json,
			last_completeness_checked_at = EXCLUDED.last_completeness_checked_at
	`, studyUID, orthancStudyID, string(locationsJSON), cacheStatus,
		report.ExpectedSeries, report.PresentSeries, string(missingSeriesJSON), report.Evaluated); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE retrieve_jobs
		SET status = 'done',
		    phase = 'done',
		    progress = 100,
		    error = NULL,
		    orthanc_study_id = NULLIF($2, ''),
		    instances_received = NULL,
		    finished_at = now()
		WHERE id = $1::uuid
	`, jobID, orthancStudyID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	event, err := a.getRetrieveJobEvent(ctx, jobID)
	if err == nil {
		a.publishRetrieveJobEvent(event)
	}
	return nil
}
