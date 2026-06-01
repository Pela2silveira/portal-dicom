package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ActionID is the stable identifier of an auditable / authorizable action.
// IDs are part of the audit/metrics contract (they end up in logs and in the
// usage_events table), so they must stay stable once shipped.
type ActionID string

const (
	ActionPatientLogin      ActionID = "patient.login"
	ActionPhysicianLogin    ActionID = "physician.login"
	ActionViewerAccessGrant ActionID = "viewer_access.grant"
	ActionStudyRetrieve     ActionID = "study.retrieve"
	ActionStudyDownload     ActionID = "study.download"
	ActionShareLinkCreate   ActionID = "share_link.create"
	ActionShareLinkRevoke   ActionID = "share_link.revoke"
	ActionShareLinkConsume  ActionID = "share_link.consume"
	ActionUsageRead         ActionID = "usage.read"
	ActionFeedbackCreate    ActionID = "feedback.create"
	ActionFeedbackRead      ActionID = "feedback.read"
)

// SubjectKind is the type of actor that can perform an action.
type SubjectKind string

const (
	SubjectPatient   SubjectKind = "patient"
	SubjectPhysician SubjectKind = "physician"
	SubjectOperator  SubjectKind = "operator"
	SubjectPublic    SubjectKind = "public"
)

// Action describes a single cross-cutting action: who may perform it (RBAC),
// whether it mutates state (same-origin enforcement), and whether it is audited
// and/or metered by default. Config can flip the audit/metrics defaults per
// deployment, but the catalog is the source of truth and lives in code.
type Action struct {
	ID         ActionID
	Resource   string
	Permission string // RBAC permission key; "" means no RBAC gate
	Subjects   []SubjectKind
	Mutating   bool
	Auditable  bool // default; overridable via observability config
	Metered    bool // default; overridable via observability config
}

func (a Action) allowsSubject(kind SubjectKind) bool {
	for _, s := range a.Subjects {
		if s == kind {
			return true
		}
	}
	return false
}

// actionCatalog is the single declarative registry of actions. Handlers refer
// to actions by ID via App.action(...). Wired today: patient/physician login,
// viewer_access.grant, study.retrieve, study.download, share_link.create and
// share_link.consume. share_link.revoke is declared and ready (no revoke
// endpoint exists yet). New actions are adopted by wrapping their handler.
var actionCatalog = map[ActionID]Action{
	ActionPatientLogin: {
		ID:        ActionPatientLogin,
		Resource:  "session",
		Subjects:  []SubjectKind{SubjectPublic},
		Mutating:  true,
		Auditable: true,
		Metered:   true,
	},
	ActionPhysicianLogin: {
		ID:        ActionPhysicianLogin,
		Resource:  "session",
		Subjects:  []SubjectKind{SubjectPublic},
		Mutating:  true,
		Auditable: true,
		Metered:   true,
	},
	ActionViewerAccessGrant: {
		ID:        ActionViewerAccessGrant,
		Resource:  "viewer_access",
		Subjects:  []SubjectKind{SubjectPatient, SubjectPhysician},
		Mutating:  true,
		Auditable: true,
		Metered:   true,
	},
	ActionStudyRetrieve: {
		ID:         ActionStudyRetrieve,
		Resource:   "study",
		Permission: "study:retrieve",
		Subjects:   []SubjectKind{SubjectPatient, SubjectPhysician},
		Mutating:   true,
		Auditable:  true,
		Metered:    true,
	},
	ActionStudyDownload: {
		ID:        ActionStudyDownload,
		Resource:  "study",
		Subjects:  []SubjectKind{SubjectPatient, SubjectPhysician},
		Mutating:  false,
		Auditable: true,
		Metered:   true,
	},
	ActionShareLinkCreate: {
		ID:         ActionShareLinkCreate,
		Resource:   "share_link",
		Permission: "share_link:create",
		Subjects:   []SubjectKind{SubjectPatient, SubjectPhysician},
		Mutating:   true,
		Auditable:  true,
		Metered:    true,
	},
	ActionShareLinkRevoke: {
		ID:         ActionShareLinkRevoke,
		Resource:   "share_link",
		Permission: "share_link:revoke",
		Subjects:   []SubjectKind{SubjectPatient, SubjectPhysician},
		Mutating:   true,
		Auditable:  true,
		Metered:    true,
	},
	ActionShareLinkConsume: {
		ID:        ActionShareLinkConsume,
		Resource:  "share_link",
		Subjects:  []SubjectKind{SubjectPublic},
		Mutating:  false,
		Auditable: true,
		Metered:   true,
	},
	// Operator-only read of usage metrics / audited actions. Resolved as a
	// physician session whose role is "operator" (rbac.operators). Audited but
	// NOT metered, to avoid the view recording usage about itself.
	ActionUsageRead: {
		ID:         ActionUsageRead,
		Resource:   "usage",
		Permission: "usage:read",
		Subjects:   []SubjectKind{SubjectPhysician},
		Mutating:   false,
		Auditable:  true,
		Metered:    false,
	},
	// Any logged-in portal user (patient or physician) can leave a comment.
	// Anonymous visitors are not allowed (no SubjectPublic), so the decorator
	// fails closed with 401. No extra RBAC permission gate; same-origin is
	// enforced because it mutates. Audited + metered like other writes.
	ActionFeedbackCreate: {
		ID:        ActionFeedbackCreate,
		Resource:  "feedback",
		Subjects:  []SubjectKind{SubjectPatient, SubjectPhysician},
		Mutating:  true,
		Auditable: true,
		Metered:   true,
	},
	// Operator-only read of submitted comments (shown at the bottom of the
	// metrics/audit view). Resolved as a physician session with operator role.
	ActionFeedbackRead: {
		ID:         ActionFeedbackRead,
		Resource:   "feedback",
		Permission: "feedback:read",
		Subjects:   []SubjectKind{SubjectPhysician},
		Mutating:   false,
		Auditable:  true,
		Metered:    false,
	},
}

// Roles known to the system. LDAP/IdP integration for roles is intentionally
// out of scope: roles are assigned via simple config lists (see RBACConfig).
const (
	RolePatient         = "patient"
	RolePhysician       = "physician"
	RolePhysicianSharer = "physician_sharer"
	RoleOperator        = "operator"
	RolePublic          = "public"

	permWildcard = "*"
)

// RBACPolicy is the resolved (config-merged) authorization policy.
type RBACPolicy struct {
	roles                map[string]map[string]bool // role -> set of permissions
	physicianSharers     map[string]bool            // lowercased username/dni -> sharer
	operators            map[string]bool            // lowercased username/dni -> operator
	physicianDefaultRole string
}

func defaultRolePermissions() map[string][]string {
	return map[string][]string{
		RolePatient:         {"share_link:create", "share_link:revoke", "study:retrieve"},
		RolePhysician:       {"study:retrieve"},
		RolePhysicianSharer: {"share_link:create", "share_link:revoke", "study:retrieve"},
		RoleOperator:        {permWildcard},
		RolePublic:          {},
	}
}

// buildRBACPolicy merges the in-code defaults with the optional config overrides.
// Roles present in config replace that role's permission set; roles absent from
// config keep their defaults. This keeps existing behaviour intact when no rbac
// section is configured (e.g. patient can always create share links).
func buildRBACPolicy(cfg *ExternalConfig) *RBACPolicy {
	policy := &RBACPolicy{
		roles:                map[string]map[string]bool{},
		physicianSharers:     map[string]bool{},
		operators:            map[string]bool{},
		physicianDefaultRole: RolePhysician,
	}

	for role, perms := range defaultRolePermissions() {
		policy.roles[role] = permSet(perms)
	}

	if cfg == nil {
		return policy
	}

	for role, perms := range cfg.RBAC.Roles {
		policy.roles[strings.TrimSpace(role)] = permSet(perms)
	}

	for _, raw := range cfg.RBAC.PhysicianSharers {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key != "" {
			policy.physicianSharers[key] = true
		}
	}

	for _, raw := range cfg.RBAC.Operators {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key != "" {
			policy.operators[key] = true
		}
	}

	if def := strings.TrimSpace(cfg.RBAC.PhysicianDefaultRole); def != "" {
		policy.physicianDefaultRole = def
	}

	return policy
}

func permSet(perms []string) map[string]bool {
	set := make(map[string]bool, len(perms))
	for _, p := range perms {
		p = strings.TrimSpace(p)
		if p != "" {
			set[p] = true
		}
	}
	return set
}

func (p *RBACPolicy) hasPermission(role, permission string) bool {
	if permission == "" {
		return true
	}
	if p == nil {
		return false
	}
	perms, ok := p.roles[role]
	if !ok {
		return false
	}
	return perms[permWildcard] || perms[permission]
}

func (p *RBACPolicy) roleForPhysician(summary PhysicianSummary) string {
	if p == nil {
		return RolePhysician
	}
	candidates := []string{summary.Username, summary.DNI}
	// Operator outranks sharer; check it across all identifiers first.
	for _, c := range candidates {
		key := strings.ToLower(strings.TrimSpace(c))
		if key != "" && p.operators[key] {
			return RoleOperator
		}
	}
	for _, c := range candidates {
		key := strings.ToLower(strings.TrimSpace(c))
		if key != "" && p.physicianSharers[key] {
			return RolePhysicianSharer
		}
	}
	return p.physicianDefaultRole
}

// Actor is the resolved identity performing an action.
type Actor struct {
	Kind        SubjectKind
	ID          string
	Role        string
	DisplayName string
	Patient     *PatientSummary
	Physician   *PhysicianSummary
}

// actionScope travels in the request context so a wrapped handler can reuse the
// actor resolved by the decorator (avoiding a second session lookup) and can
// attach action-specific dimensions (e.g. study_uid) to the audit/metric record.
type actionScope struct {
	actor Actor
	dims  map[string]any
}

type actionScopeKey struct{}

func withActionScope(ctx context.Context, scope *actionScope) context.Context {
	return context.WithValue(ctx, actionScopeKey{}, scope)
}

func actionScopeFromContext(ctx context.Context) *actionScope {
	scope, _ := ctx.Value(actionScopeKey{}).(*actionScope)
	return scope
}

// actorFromContext returns the actor resolved by the action() decorator, if any.
func actorFromContext(ctx context.Context) (Actor, bool) {
	scope := actionScopeFromContext(ctx)
	if scope == nil {
		return Actor{}, false
	}
	return scope.actor, true
}

// setActionDim lets a wrapped handler enrich the audit/metric record for the
// current action (no-op if the request did not go through the decorator).
func setActionDim(ctx context.Context, key string, value any) {
	scope := actionScopeFromContext(ctx)
	if scope == nil {
		return
	}
	if scope.dims == nil {
		scope.dims = map[string]any{}
	}
	scope.dims[key] = value
}

// resolveActor tries to resolve the request's actor against the action's allowed
// subjects, in declaration order. It fails closed for protected actions and
// returns a synthetic public actor for actions that explicitly allow it.
func (a *App) resolveActor(ctx context.Context, r *http.Request, act Action) (Actor, bool) {
	for _, kind := range act.Subjects {
		switch kind {
		case SubjectPatient:
			if _, summary, err := a.requirePatientSessionSummary(ctx, r); err == nil {
				s := summary
				return Actor{
					Kind:        SubjectPatient,
					ID:          summary.ID,
					Role:        RolePatient,
					DisplayName: summary.FullName,
					Patient:     &s,
				}, true
			}
		case SubjectPhysician:
			if _, summary, err := a.requirePhysicianSessionSummary(ctx, r); err == nil {
				s := summary
				return Actor{
					Kind:        SubjectPhysician,
					ID:          summary.ID,
					Role:        a.rbac.roleForPhysician(summary),
					DisplayName: summary.FullName,
					Physician:   &s,
				}, true
			}
		}
	}
	if act.allowsSubject(SubjectPublic) {
		return Actor{Kind: SubjectPublic, Role: RolePublic}, true
	}
	return Actor{}, false
}

// statusRecorder captures the response status code so the decorator can derive
// the action outcome for audit/metrics without changing handler signatures.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rec *statusRecorder) WriteHeader(code int) {
	if !rec.wroteHeader {
		rec.status = code
		rec.wroteHeader = true
	}
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	if !rec.wroteHeader {
		rec.status = http.StatusOK
		rec.wroteHeader = true
	}
	return rec.ResponseWriter.Write(b)
}

func (rec *statusRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func outcomeForStatus(status int) string {
	switch {
	case status >= 200 && status < 400:
		return "success"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "denied"
	default:
		return "failure"
	}
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// action wraps an HTTP handler with the cross-cutting concerns of an Action:
// same-origin enforcement (for mutating actions), actor resolution, RBAC, and
// audit + usage-metric emission. Unknown action IDs pass through untouched.
func (a *App) action(id ActionID, next http.HandlerFunc) http.HandlerFunc {
	act, known := actionCatalog[id]
	return func(w http.ResponseWriter, r *http.Request) {
		if !known {
			next(w, r)
			return
		}

		if act.Mutating && isMutatingMethod(r.Method) && !sameOriginRequest(r) {
			a.log("warn", "browser_origin_rejected", map[string]any{
				"action":      string(act.ID),
				"method":      r.Method,
				"path":        r.URL.Path,
				"origin":      strings.TrimSpace(r.Header.Get("Origin")),
				"base_origin": requestBaseOrigin(r),
			})
			http.Error(w, "cross-site request rejected", http.StatusForbidden)
			return
		}

		actor, ok := a.resolveActor(r.Context(), r, act)
		if !ok {
			a.emitAuditDeny(r, act, actor, "unauthenticated")
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}

		if !a.rbac.hasPermission(actor.Role, act.Permission) {
			a.emitAuditDeny(r, act, actor, "forbidden")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		scope := &actionScope{actor: actor, dims: map[string]any{}}
		r = r.WithContext(withActionScope(r.Context(), scope))

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next(rec, r)
		latency := time.Since(start)

		a.recordActionObservability(r, act, scope.actor, scope.dims, rec.status, latency)
	}
}

func (a *App) auditEnabledFor(act Action) bool {
	if a.externalConfig != nil {
		obs := a.externalConfig.Observability
		if obs.AuditEnabled != nil && !*obs.AuditEnabled {
			return false
		}
		if len(obs.AuditActions) > 0 {
			return containsString(obs.AuditActions, string(act.ID))
		}
	}
	return act.Auditable
}

func (a *App) metricsEnabledFor(act Action) bool {
	if a.externalConfig != nil {
		obs := a.externalConfig.Observability
		if obs.MetricsEnabled != nil && !*obs.MetricsEnabled {
			return false
		}
		if len(obs.MeteredActions) > 0 {
			return containsString(obs.MeteredActions, string(act.ID))
		}
	}
	return act.Metered
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

// audit emits a structured JSON line on the "audit" channel (stdout). The
// host-side portal-logs-audit systemd streamer routes these to app/logs/audit.log.
func (a *App) audit(action ActionID, fields map[string]any) {
	payload := map[string]any{"channel": "audit", "action": string(action)}
	for k, v := range fields {
		payload[k] = v
	}
	a.log("info", "audit_event", payload)
}

func (a *App) emitAuditDeny(r *http.Request, act Action, actor Actor, reason string) {
	if !a.auditEnabledFor(act) {
		return
	}
	a.audit(act.ID, map[string]any{
		"outcome":    "denied",
		"reason":     reason,
		"actor_kind": string(actor.Kind),
		"actor_id":   actor.ID,
		"actor_role": actor.Role,
		"resource":   act.Resource,
		"method":     r.Method,
		"path":       r.URL.Path,
		"client_ip":  clientIPForRateLimit(r),
	})
}

func (a *App) recordActionObservability(r *http.Request, act Action, actor Actor, dims map[string]any, status int, latency time.Duration) {
	outcome := outcomeForStatus(status)
	latencyMS := latency.Milliseconds()

	if a.auditEnabledFor(act) {
		fields := map[string]any{
			"outcome":     outcome,
			"status_code": status,
			"actor_kind":  string(actor.Kind),
			"actor_id":    actor.ID,
			"actor_role":  actor.Role,
			"actor_name":  actor.DisplayName,
			"resource":    act.Resource,
			"method":      r.Method,
			"path":        r.URL.Path,
			"client_ip":   clientIPForRateLimit(r),
			"latency_ms":  latencyMS,
		}
		for k, v := range dims {
			fields[k] = v
		}
		a.audit(act.ID, fields)
	}

	if a.metricsEnabledFor(act) && a.usageRecorder != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := a.usageRecorder.Record(ctx, UsageEvent{
			Action:     act.ID,
			ActorKind:  actor.Kind,
			ActorID:    actor.ID,
			ActorRole:  actor.Role,
			Outcome:    outcome,
			StatusCode: status,
			LatencyMS:  latencyMS,
			Dims:       dims,
			OccurredAt: time.Now().UTC(),
		})
		if err != nil {
			a.log("error", "usage_event_record_failed", map[string]any{
				"action": string(act.ID),
				"error":  err.Error(),
			})
		}
	}
}

// UsageEvent is one append-only row of the usage_events table (Option B: usage
// metrics live inside the product database, separate from operational tables).
type UsageEvent struct {
	Action     ActionID
	ActorKind  SubjectKind
	ActorID    string
	ActorRole  string
	Outcome    string
	StatusCode int
	LatencyMS  int64
	Dims       map[string]any
	OccurredAt time.Time
}

// UsageRecorder is the sink for usage metrics. The Postgres implementation
// writes to usage_events; tests use an in-memory fake (interface seam, no
// new dependency required).
type UsageRecorder interface {
	Record(ctx context.Context, evt UsageEvent) error
}

type PostgresUsageRecorder struct {
	db *sql.DB
}

func (p *PostgresUsageRecorder) Record(ctx context.Context, evt UsageEvent) error {
	if p == nil || p.db == nil {
		return nil
	}
	dims := evt.Dims
	if dims == nil {
		dims = map[string]any{}
	}
	dimsJSON, err := json.Marshal(dims)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `
		INSERT INTO usage_events
			(action, actor_kind, actor_id, actor_role, outcome, status_code, latency_ms, dims, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		string(evt.Action),
		string(evt.ActorKind),
		nullIfEmpty(evt.ActorID),
		nullIfEmpty(evt.ActorRole),
		evt.Outcome,
		evt.StatusCode,
		evt.LatencyMS,
		dimsJSON,
		evt.OccurredAt,
	)
	return err
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
