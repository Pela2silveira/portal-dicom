package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// operator.go exposes the read-only operator view over the usage_events table
// (the same rows the audit/metrics decorator writes). Access is gated by the
// usage.read action, which only the operator role (rbac.operators) can perform.

const (
	usageEventsDefaultLimit = 100
	usageEventsMaxLimit     = 500
)

type usageWindow struct {
	label    string
	duration time.Duration
}

// normalizeUsageWindow maps the supported window tokens to a duration. Defaults
// to 7d for unknown/empty input so the view always has a bounded scan.
func normalizeUsageWindow(raw string) usageWindow {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "24h", "1d", "day":
		return usageWindow{label: "24h", duration: 24 * time.Hour}
	case "30d", "month":
		return usageWindow{label: "30d", duration: 30 * 24 * time.Hour}
	case "7d", "week", "":
		return usageWindow{label: "7d", duration: 7 * 24 * time.Hour}
	default:
		return usageWindow{label: "7d", duration: 7 * 24 * time.Hour}
	}
}

type usageActionStat struct {
	Action     string `json:"action"`
	Total      int    `json:"total"`
	Success    int    `json:"success"`
	Denied     int    `json:"denied"`
	Failure    int    `json:"failure"`
	AvgLatency int64  `json:"avg_latency_ms"`
	P95Latency int64  `json:"p95_latency_ms"`
}

type usageOutcomeStat struct {
	Outcome string `json:"outcome"`
	Total   int    `json:"total"`
}

type usageStatusStat struct {
	StatusCode int `json:"status_code"`
	Total      int `json:"total"`
}

type usageModalityStat struct {
	Modality string `json:"modality"`
	Total    int    `json:"total"`
}

type usageRetrieverStat struct {
	DNI   string `json:"dni"`
	Total int    `json:"total"`
}

// usageLoginStats are login-derived counters. Login events are recorded with a
// public actor (the session does not exist yet), so the distinct counts come
// from the identity stashed in dims at login time: patient_id for patients and
// identifier (username) for physicians.
type usageLoginStats struct {
	PatientLogins         int `json:"patient_logins"`
	UniquePatients        int `json:"unique_patients"`
	PhysicianLogins       int `json:"physician_logins"`
	UniquePhysicianLogins int `json:"unique_physician_logins"`
}

type usageActorStat struct {
	ActorKind string `json:"actor_kind"`
	ActorRole string `json:"actor_role"`
	ActorID   string `json:"actor_id"`
	Total     int    `json:"total"`
}

type usageSummaryResponse struct {
	Window                 string               `json:"window"`
	Since                  time.Time            `json:"since"`
	Total                  int                  `json:"total"`
	Logins                 usageLoginStats      `json:"logins"`
	ByAction               []usageActionStat    `json:"by_action"`
	ByOutcome              []usageOutcomeStat   `json:"by_outcome"`
	ByStatus               []usageStatusStat    `json:"by_status"`
	DownloadsByModality    []usageModalityStat  `json:"downloads_by_modality"`
	RetrievesByModality    []usageModalityStat  `json:"retrieves_by_modality"`
	TopPhysicianRetrievers []usageRetrieverStat `json:"top_physician_retrievers"`
	TopActors              []usageActorStat     `json:"top_actors"`
}

type usageEventRow struct {
	ID         int64           `json:"id"`
	Action     string          `json:"action"`
	ActorKind  string          `json:"actor_kind"`
	ActorID    string          `json:"actor_id"`
	ActorRole  string          `json:"actor_role"`
	Outcome    string          `json:"outcome"`
	StatusCode int             `json:"status_code"`
	LatencyMS  int64           `json:"latency_ms"`
	Dims       json.RawMessage `json:"dims"`
	OccurredAt time.Time       `json:"occurred_at"`
}

type usageEventsResponse struct {
	Window  string          `json:"window"`
	Since   time.Time       `json:"since"`
	Action  string          `json:"action,omitempty"`
	Outcome string          `json:"outcome,omitempty"`
	Query   string          `json:"q,omitempty"`
	Limit   int             `json:"limit"`
	Events  []usageEventRow `json:"events"`
}

func (a *App) handleOperatorUsageSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.db == nil {
		http.Error(w, "metrics store unavailable", http.StatusServiceUnavailable)
		return
	}

	window := normalizeUsageWindow(r.URL.Query().Get("window"))
	since := time.Now().UTC().Add(-window.duration)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	summary, err := a.queryUsageSummary(ctx, since)
	if err != nil {
		a.log("error", "usage_summary_query_failed", map[string]any{"error": err.Error()})
		http.Error(w, "failed to load usage summary", http.StatusInternalServerError)
		return
	}
	summary.Window = window.label
	summary.Since = since
	setActionDim(r.Context(), "window", window.label)
	writeJSON(w, http.StatusOK, summary)
}

func (a *App) handleOperatorUsageEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.db == nil {
		http.Error(w, "metrics store unavailable", http.StatusServiceUnavailable)
		return
	}

	window := normalizeUsageWindow(r.URL.Query().Get("window"))
	since := time.Now().UTC().Add(-window.duration)
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	outcome := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("outcome")))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := parseUsageLimit(r.URL.Query().Get("limit"))

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	events, err := a.queryUsageEvents(ctx, since, action, outcome, query, limit)
	if err != nil {
		a.log("error", "usage_events_query_failed", map[string]any{"error": err.Error()})
		http.Error(w, "failed to load usage events", http.StatusInternalServerError)
		return
	}
	setActionDim(r.Context(), "window", window.label)
	writeJSON(w, http.StatusOK, usageEventsResponse{
		Window:  window.label,
		Since:   since,
		Action:  action,
		Outcome: outcome,
		Query:   query,
		Limit:   limit,
		Events:  events,
	})
}

func parseUsageLimit(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return usageEventsDefaultLimit
	}
	if n > usageEventsMaxLimit {
		return usageEventsMaxLimit
	}
	return n
}

func (a *App) queryUsageSummary(ctx context.Context, since time.Time) (usageSummaryResponse, error) {
	summary := usageSummaryResponse{
		ByAction:               []usageActionStat{},
		ByOutcome:              []usageOutcomeStat{},
		ByStatus:               []usageStatusStat{},
		DownloadsByModality:    []usageModalityStat{},
		RetrievesByModality:    []usageModalityStat{},
		TopPhysicianRetrievers: []usageRetrieverStat{},
		TopActors:              []usageActorStat{},
	}

	actionRows, err := a.db.QueryContext(ctx, `
		SELECT action,
		       count(*) AS total,
		       count(*) FILTER (WHERE outcome = 'success') AS success,
		       count(*) FILTER (WHERE outcome = 'denied')  AS denied,
		       count(*) FILTER (WHERE outcome = 'failure') AS failure,
		       COALESCE(avg(latency_ms), 0)::bigint AS avg_latency,
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms), 0)::bigint AS p95_latency
		FROM usage_events
		WHERE occurred_at >= $1
		GROUP BY action
		ORDER BY total DESC
	`, since)
	if err != nil {
		return summary, err
	}
	defer actionRows.Close()
	for actionRows.Next() {
		var stat usageActionStat
		if err := actionRows.Scan(&stat.Action, &stat.Total, &stat.Success, &stat.Denied, &stat.Failure, &stat.AvgLatency, &stat.P95Latency); err != nil {
			return summary, err
		}
		summary.Total += stat.Total
		summary.ByAction = append(summary.ByAction, stat)
	}
	if err := actionRows.Err(); err != nil {
		return summary, err
	}

	outcomeRows, err := a.db.QueryContext(ctx, `
		SELECT outcome, count(*) AS total
		FROM usage_events
		WHERE occurred_at >= $1
		GROUP BY outcome
		ORDER BY total DESC
	`, since)
	if err != nil {
		return summary, err
	}
	defer outcomeRows.Close()
	for outcomeRows.Next() {
		var stat usageOutcomeStat
		if err := outcomeRows.Scan(&stat.Outcome, &stat.Total); err != nil {
			return summary, err
		}
		summary.ByOutcome = append(summary.ByOutcome, stat)
	}
	if err := outcomeRows.Err(); err != nil {
		return summary, err
	}

	statusRows, err := a.db.QueryContext(ctx, `
		SELECT COALESCE(status_code, 0) AS status_code, count(*) AS total
		FROM usage_events
		WHERE occurred_at >= $1
		GROUP BY status_code
		ORDER BY status_code
	`, since)
	if err != nil {
		return summary, err
	}
	defer statusRows.Close()
	for statusRows.Next() {
		var stat usageStatusStat
		if err := statusRows.Scan(&stat.StatusCode, &stat.Total); err != nil {
			return summary, err
		}
		summary.ByStatus = append(summary.ByStatus, stat)
	}
	if err := statusRows.Err(); err != nil {
		return summary, err
	}

	if err := a.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE action = 'patient.login') AS patient_logins,
			COUNT(DISTINCT (dims->>'patient_id')) FILTER (
				WHERE action = 'patient.login' AND COALESCE(dims->>'patient_id', '') <> ''
			) AS unique_patients,
			COUNT(*) FILTER (WHERE action = 'physician.login') AS physician_logins,
			COUNT(DISTINCT (dims->>'identifier')) FILTER (
				WHERE action = 'physician.login' AND COALESCE(dims->>'identifier', '') <> ''
			) AS unique_physician_logins
		FROM usage_events
		WHERE occurred_at >= $1
	`, since).Scan(
		&summary.Logins.PatientLogins,
		&summary.Logins.UniquePatients,
		&summary.Logins.PhysicianLogins,
		&summary.Logins.UniquePhysicianLogins,
	); err != nil {
		return summary, err
	}

	if summary.DownloadsByModality, err = a.queryModalityBreakdown(ctx, since, "study.download"); err != nil {
		return summary, err
	}
	if summary.RetrievesByModality, err = a.queryModalityBreakdown(ctx, since, "study.retrieve"); err != nil {
		return summary, err
	}

	retrieverRows, err := a.db.QueryContext(ctx, `
		SELECT dims->>'physician_dni' AS dni, count(*) AS total
		FROM usage_events
		WHERE occurred_at >= $1 AND action = 'study.retrieve' AND actor_kind = 'physician'
		  AND COALESCE(dims->>'physician_dni', '') <> ''
		GROUP BY 1
		ORDER BY total DESC
		LIMIT 10
	`, since)
	if err != nil {
		return summary, err
	}
	defer retrieverRows.Close()
	for retrieverRows.Next() {
		var stat usageRetrieverStat
		if err := retrieverRows.Scan(&stat.DNI, &stat.Total); err != nil {
			return summary, err
		}
		summary.TopPhysicianRetrievers = append(summary.TopPhysicianRetrievers, stat)
	}
	if err := retrieverRows.Err(); err != nil {
		return summary, err
	}

	actorRows, err := a.db.QueryContext(ctx, `
		SELECT actor_kind,
		       COALESCE(actor_role, '') AS actor_role,
		       COALESCE(actor_id, '')   AS actor_id,
		       count(*) AS total
		FROM usage_events
		WHERE occurred_at >= $1 AND actor_id IS NOT NULL
		GROUP BY actor_kind, actor_role, actor_id
		ORDER BY total DESC
		LIMIT 10
	`, since)
	if err != nil {
		return summary, err
	}
	defer actorRows.Close()
	for actorRows.Next() {
		var stat usageActorStat
		if err := actorRows.Scan(&stat.ActorKind, &stat.ActorRole, &stat.ActorID, &stat.Total); err != nil {
			return summary, err
		}
		summary.TopActors = append(summary.TopActors, stat)
	}
	if err := actorRows.Err(); err != nil {
		return summary, err
	}

	return summary, nil
}

// queryModalityBreakdown counts successful events of the given action grouped
// by their `modality` dim (UNKNOWN when missing). Used for both downloads and
// retrieves by modality.
func (a *App) queryModalityBreakdown(ctx context.Context, since time.Time, action string) ([]usageModalityStat, error) {
	stats := []usageModalityStat{}
	rows, err := a.db.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(dims->>'modality', ''), 'UNKNOWN') AS modality, count(*) AS total
		FROM usage_events
		WHERE occurred_at >= $1 AND action = $2 AND outcome = 'success'
		GROUP BY 1
		ORDER BY total DESC
	`, since, action)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var stat usageModalityStat
		if err := rows.Scan(&stat.Modality, &stat.Total); err != nil {
			return stats, err
		}
		stats = append(stats, stat)
	}
	return stats, rows.Err()
}

func (a *App) queryUsageEvents(ctx context.Context, since time.Time, action, outcome, query string, limit int) ([]usageEventRow, error) {
	events := []usageEventRow{}
	rows, err := a.db.QueryContext(ctx, `
		SELECT id,
		       action,
		       actor_kind,
		       COALESCE(actor_id, '')    AS actor_id,
		       COALESCE(actor_role, '')  AS actor_role,
		       outcome,
		       COALESCE(status_code, 0)  AS status_code,
		       COALESCE(latency_ms, 0)   AS latency_ms,
		       dims,
		       occurred_at
		FROM usage_events
		WHERE occurred_at >= $1
		  AND ($2 = '' OR action = $2)
		  AND ($3 = '' OR outcome = $3)
		  AND ($4 = '' OR position(lower($4) in lower(dims::text)) > 0)
		ORDER BY occurred_at DESC, id DESC
		LIMIT $5
	`, since, action, outcome, query, limit)
	if err != nil {
		return events, err
	}
	defer rows.Close()
	for rows.Next() {
		var evt usageEventRow
		var dims []byte
		if err := rows.Scan(&evt.ID, &evt.Action, &evt.ActorKind, &evt.ActorID, &evt.ActorRole, &evt.Outcome, &evt.StatusCode, &evt.LatencyMS, &dims, &evt.OccurredAt); err != nil {
			return events, err
		}
		if len(dims) == 0 {
			dims = []byte("{}")
		}
		evt.Dims = json.RawMessage(dims)
		events = append(events, evt)
	}
	if err := rows.Err(); err != nil {
		return events, err
	}
	return events, nil
}

// usageModalityDim collapses a study's modality list into a single dimension
// value for the study.download usage event (so the operator view can break
// downloads down by modality). Multi-modality studies keep all modalities
// joined with "/"; empty input becomes "UNKNOWN".
func usageModalityDim(modalities []string) string {
	cleaned := make([]string, 0, len(modalities))
	seen := map[string]bool{}
	for _, m := range modalities {
		m = strings.ToUpper(strings.TrimSpace(m))
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		cleaned = append(cleaned, m)
	}
	if len(cleaned) == 0 {
		return "UNKNOWN"
	}
	// Sort so the dim is order-independent: a CT+MR study always yields "CT/MR"
	// regardless of the order modalities arrived in, and matches the historical
	// backfill (which sorts in SQL). Avoids splitting the same combo into
	// distinct breakdown buckets.
	sort.Strings(cleaned)
	return strings.Join(cleaned, "/")
}

// physicianCanViewMetrics reports whether the physician's resolved RBAC role
// grants the usage:read permission (i.e. listed in rbac.operators).
func (a *App) physicianCanViewMetrics(summary PhysicianSummary) bool {
	if a.rbac == nil {
		return false
	}
	return a.rbac.hasPermission(a.rbac.roleForPhysician(summary), actionCatalog[ActionUsageRead].Permission)
}
