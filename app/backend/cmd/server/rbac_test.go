package main

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeUsageRecorder struct {
	events []UsageEvent
	err    error
}

func (f *fakeUsageRecorder) Record(_ context.Context, evt UsageEvent) error {
	f.events = append(f.events, evt)
	return f.err
}

func boolPtr(b bool) *bool { return &b }

func newSameOriginRequest(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Host = "portal.test"
	r.Header.Set("Origin", "http://portal.test")
	return r
}

func TestBuildRBACPolicyDefaults(t *testing.T) {
	policy := buildRBACPolicy(nil)

	if !policy.hasPermission(RolePatient, "share_link:create") {
		t.Fatalf("patient should be able to create share links by default")
	}
	if policy.hasPermission(RolePhysician, "share_link:create") {
		t.Fatalf("plain physician should NOT be able to create share links by default")
	}
	if !policy.hasPermission(RolePhysicianSharer, "share_link:create") {
		t.Fatalf("physician_sharer should be able to create share links")
	}
	if !policy.hasPermission(RoleOperator, "anything:at:all") {
		t.Fatalf("operator wildcard should grant any permission")
	}
	if policy.hasPermission(RolePublic, "share_link:create") {
		t.Fatalf("public role should not have share permission")
	}
	if !policy.hasPermission(RolePublic, "") {
		t.Fatalf("empty permission must always pass (ungated action)")
	}
	if policy.hasPermission("ghost_role", "share_link:create") {
		t.Fatalf("unknown role must be denied")
	}
}

func TestBuildRBACPolicyConfigOverride(t *testing.T) {
	cfg := &ExternalConfig{
		RBAC: RBACConfig{
			Roles: map[string][]string{
				// Override: take share away from patient for this deployment.
				RolePatient: {"study:retrieve"},
			},
			PhysicianSharers:     []string{"DrHouse", "  20111222 "},
			PhysicianDefaultRole: "physician",
		},
	}
	policy := buildRBACPolicy(cfg)

	if policy.hasPermission(RolePatient, "share_link:create") {
		t.Fatalf("config override should remove patient share permission")
	}
	if !policy.hasPermission(RolePatient, "study:retrieve") {
		t.Fatalf("config override should keep study:retrieve for patient")
	}
	// Roles not present in config keep their defaults.
	if !policy.hasPermission(RolePhysicianSharer, "share_link:create") {
		t.Fatalf("physician_sharer defaults should survive partial config")
	}

	// physician_sharers matching is case-insensitive and trimmed.
	if got := policy.roleForPhysician(PhysicianSummary{Username: "drhouse"}); got != RolePhysicianSharer {
		t.Fatalf("expected sharer role by username, got %q", got)
	}
	if got := policy.roleForPhysician(PhysicianSummary{DNI: "20111222"}); got != RolePhysicianSharer {
		t.Fatalf("expected sharer role by dni, got %q", got)
	}
	if got := policy.roleForPhysician(PhysicianSummary{Username: "someone-else"}); got != RolePhysician {
		t.Fatalf("expected default physician role, got %q", got)
	}
}

func TestRoleForPhysicianCustomDefault(t *testing.T) {
	cfg := &ExternalConfig{
		RBAC: RBACConfig{PhysicianDefaultRole: "physician_sharer"},
	}
	policy := buildRBACPolicy(cfg)
	if got := policy.roleForPhysician(PhysicianSummary{Username: "anyone"}); got != RolePhysicianSharer {
		t.Fatalf("custom default role should apply, got %q", got)
	}
}

func TestAuditMetricsToggles(t *testing.T) {
	create := actionCatalog[ActionShareLinkCreate]

	// nil config -> use catalog defaults.
	app := &App{}
	if !app.auditEnabledFor(create) || !app.metricsEnabledFor(create) {
		t.Fatalf("defaults should be enabled for share_link.create")
	}

	// master switch off.
	app = &App{externalConfig: &ExternalConfig{Observability: ObservabilityConfig{
		AuditEnabled:   boolPtr(false),
		MetricsEnabled: boolPtr(false),
	}}}
	if app.auditEnabledFor(create) || app.metricsEnabledFor(create) {
		t.Fatalf("master switch off should disable both")
	}

	// explicit allowlist overrides per-action defaults.
	app = &App{externalConfig: &ExternalConfig{Observability: ObservabilityConfig{
		AuditActions:   []string{"patient.login"},
		MeteredActions: []string{string(ActionShareLinkCreate)},
	}}}
	if app.auditEnabledFor(create) {
		t.Fatalf("create should not be audited when allowlist excludes it")
	}
	if !app.metricsEnabledFor(create) {
		t.Fatalf("create should be metered when allowlist includes it")
	}
}

func TestOutcomeForStatus(t *testing.T) {
	cases := map[int]string{
		200: "success",
		302: "success",
		401: "denied",
		403: "denied",
		400: "failure",
		500: "failure",
	}
	for status, want := range cases {
		if got := outcomeForStatus(status); got != want {
			t.Fatalf("status %d: want %q got %q", status, want, got)
		}
	}
}

func TestActionDecoratorPublicSuccess(t *testing.T) {
	buf := &bytes.Buffer{}
	rec := &fakeUsageRecorder{}
	app := &App{
		logger:        log.New(buf, "", 0),
		rbac:          buildRBACPolicy(nil),
		usageRecorder: rec,
	}

	handler := app.action(ActionShareLinkConsume, func(w http.ResponseWriter, r *http.Request) {
		setActionDim(r.Context(), "token_present", true)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/share?token=abc", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if len(rec.events) != 1 {
		t.Fatalf("want 1 usage event, got %d", len(rec.events))
	}
	evt := rec.events[0]
	if evt.Action != ActionShareLinkConsume || evt.ActorKind != SubjectPublic || evt.Outcome != "success" || evt.StatusCode != 200 {
		t.Fatalf("unexpected usage event: %+v", evt)
	}
	if evt.Dims["token_present"] != true {
		t.Fatalf("handler dimension not propagated to metric: %+v", evt.Dims)
	}
	out := buf.String()
	if !strings.Contains(out, `"channel":"audit"`) || !strings.Contains(out, `"action":"share_link.consume"`) || !strings.Contains(out, `"outcome":"success"`) {
		t.Fatalf("audit line missing expected fields: %s", out)
	}
}

func TestActionDecoratorUnauthenticatedDeny(t *testing.T) {
	buf := &bytes.Buffer{}
	rec := &fakeUsageRecorder{}
	app := &App{
		logger:        log.New(buf, "", 0),
		rbac:          buildRBACPolicy(nil),
		usageRecorder: rec,
	}

	called := false
	handler := app.action(ActionShareLinkCreate, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	w := httptest.NewRecorder()
	// same-origin POST with no session cookies -> no actor resolvable.
	handler(w, newSameOriginRequest(http.MethodPost, "/api/patient/studies/1.2.3/share"))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	if called {
		t.Fatalf("inner handler must not run when unauthenticated")
	}
	if len(rec.events) != 0 {
		t.Fatalf("no usage metric expected on auth failure, got %d", len(rec.events))
	}
	out := buf.String()
	if !strings.Contains(out, `"channel":"audit"`) || !strings.Contains(out, `"outcome":"denied"`) || !strings.Contains(out, `"reason":"unauthenticated"`) {
		t.Fatalf("expected deny audit line, got: %s", out)
	}
}

func TestActionDecoratorCrossOriginRejected(t *testing.T) {
	app := &App{
		logger: log.New(&bytes.Buffer{}, "", 0),
		rbac:   buildRBACPolicy(nil),
	}

	called := false
	handler := app.action(ActionShareLinkCreate, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	r := httptest.NewRequest(http.MethodPost, "/api/patient/studies/1.2.3/share", nil)
	r.Host = "portal.test"
	r.Header.Set("Origin", "http://evil.test")

	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
	if called {
		t.Fatalf("inner handler must not run on cross-origin request")
	}
}

func TestActionContextHelpers(t *testing.T) {
	ctx := context.Background()
	if _, ok := actorFromContext(ctx); ok {
		t.Fatalf("no actor expected in bare context")
	}
	// setActionDim on a bare context is a no-op (does not panic).
	setActionDim(ctx, "k", "v")

	scope := &actionScope{actor: Actor{Kind: SubjectPatient, ID: "p1", Role: RolePatient}}
	ctx = withActionScope(ctx, scope)

	actor, ok := actorFromContext(ctx)
	if !ok || actor.ID != "p1" || actor.Kind != SubjectPatient {
		t.Fatalf("actor not recovered from context: %+v ok=%v", actor, ok)
	}
	setActionDim(ctx, "study_uid", "1.2.3")
	if scope.dims["study_uid"] != "1.2.3" {
		t.Fatalf("dimension not stored: %+v", scope.dims)
	}
}

func TestActionDecoratorForbidden(t *testing.T) {
	// Register a temporary catalog entry gated by a permission that no role
	// (in particular "public") holds, so we can exercise the RBAC-deny branch
	// of the decorator without a real authenticated session.
	const gated ActionID = "test.gated.forbidden"
	actionCatalog[gated] = Action{
		ID:         gated,
		Resource:   "test",
		Permission: "test:forbidden",
		Subjects:   []SubjectKind{SubjectPublic},
		Auditable:  true,
		Metered:    true,
	}
	defer delete(actionCatalog, gated)

	buf := &bytes.Buffer{}
	rec := &fakeUsageRecorder{}
	app := &App{logger: log.New(buf, "", 0), rbac: buildRBACPolicy(nil), usageRecorder: rec}

	called := false
	handler := app.action(gated, func(w http.ResponseWriter, r *http.Request) { called = true })

	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/whatever", nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
	if called {
		t.Fatalf("inner handler must not run when forbidden")
	}
	if len(rec.events) != 0 {
		t.Fatalf("no usage metric expected on RBAC deny, got %d", len(rec.events))
	}
	out := buf.String()
	if !strings.Contains(out, `"outcome":"denied"`) || !strings.Contains(out, `"reason":"forbidden"`) {
		t.Fatalf("expected forbidden deny audit line, got: %s", out)
	}
}

func TestActionDecoratorObservabilityTogglesOff(t *testing.T) {
	buf := &bytes.Buffer{}
	rec := &fakeUsageRecorder{}
	app := &App{
		logger:        log.New(buf, "", 0),
		rbac:          buildRBACPolicy(nil),
		usageRecorder: rec,
		externalConfig: &ExternalConfig{Observability: ObservabilityConfig{
			AuditEnabled:   boolPtr(false),
			MetricsEnabled: boolPtr(false),
		}},
	}

	handler := app.action(ActionShareLinkConsume, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/share?t=abc", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if len(rec.events) != 0 {
		t.Fatalf("metrics disabled: expected 0 usage events, got %d", len(rec.events))
	}
	if strings.Contains(buf.String(), "audit_event") {
		t.Fatalf("audit disabled: no audit line expected, got: %s", buf.String())
	}
}

func TestActionDecoratorFailureOutcome(t *testing.T) {
	rec := &fakeUsageRecorder{}
	app := &App{logger: log.New(&bytes.Buffer{}, "", 0), rbac: buildRBACPolicy(nil), usageRecorder: rec}

	handler := app.action(ActionShareLinkConsume, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/share?t=abc", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
	if len(rec.events) != 1 {
		t.Fatalf("want 1 usage event, got %d", len(rec.events))
	}
	if evt := rec.events[0]; evt.Outcome != "failure" || evt.StatusCode != 500 {
		t.Fatalf("expected failure/500 outcome, got %+v", evt)
	}
}

func TestActionDecoratorRecorderErrorIsNonFatal(t *testing.T) {
	buf := &bytes.Buffer{}
	rec := &fakeUsageRecorder{err: context.DeadlineExceeded}
	app := &App{logger: log.New(buf, "", 0), rbac: buildRBACPolicy(nil), usageRecorder: rec}

	handler := app.action(ActionShareLinkConsume, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/share?t=abc", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("recorder error must not change response, got %d", w.Code)
	}
	if !strings.Contains(buf.String(), "usage_event_record_failed") {
		t.Fatalf("expected recorder error to be logged, got: %s", buf.String())
	}
}

func TestPostgresUsageRecorderNilSafe(t *testing.T) {
	var p *PostgresUsageRecorder
	if err := p.Record(context.Background(), UsageEvent{}); err != nil {
		t.Fatalf("nil recorder should be a no-op, got %v", err)
	}
	p = &PostgresUsageRecorder{db: nil}
	if err := p.Record(context.Background(), UsageEvent{}); err != nil {
		t.Fatalf("recorder with nil db should be a no-op, got %v", err)
	}
}
