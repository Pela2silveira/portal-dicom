package main

import (
	"strings"
	"testing"
	"time"
)

func TestRoleForPhysicianOperator(t *testing.T) {
	cfg := &ExternalConfig{
		RBAC: RBACConfig{
			PhysicianSharers: []string{"20111222"},
			Operators:        []string{"31595723"},
		},
	}
	policy := buildRBACPolicy(cfg)

	if got := policy.roleForPhysician(PhysicianSummary{Username: "31595723"}); got != RoleOperator {
		t.Fatalf("configured operator should resolve to operator role, got %q", got)
	}
	if got := policy.roleForPhysician(PhysicianSummary{DNI: "31595723"}); got != RoleOperator {
		t.Fatalf("operator by DNI should resolve to operator role, got %q", got)
	}
	if got := policy.roleForPhysician(PhysicianSummary{Username: "20111222"}); got != RolePhysicianSharer {
		t.Fatalf("sharer should resolve to sharer role, got %q", got)
	}
	if got := policy.roleForPhysician(PhysicianSummary{Username: "99999999"}); got != RolePhysician {
		t.Fatalf("regular physician should resolve to default role, got %q", got)
	}

	// Operator outranks sharer even when matched via different identifiers.
	both := buildRBACPolicy(&ExternalConfig{RBAC: RBACConfig{
		PhysicianSharers: []string{"sharername"},
		Operators:        []string{"31595723"},
	}})
	if got := both.roleForPhysician(PhysicianSummary{Username: "sharername", DNI: "31595723"}); got != RoleOperator {
		t.Fatalf("operator must outrank sharer, got %q", got)
	}
}

func TestUsageReadPermission(t *testing.T) {
	policy := buildRBACPolicy(&ExternalConfig{})

	if !policy.hasPermission(RoleOperator, "usage:read") {
		t.Fatalf("operator (wildcard) must be granted usage:read")
	}
	for _, role := range []string{RolePhysician, RolePhysicianSharer, RolePatient, RolePublic} {
		if policy.hasPermission(role, "usage:read") {
			t.Fatalf("role %q must NOT be granted usage:read", role)
		}
	}
}

func TestPhysicianCanViewMetrics(t *testing.T) {
	app := &App{rbac: buildRBACPolicy(&ExternalConfig{RBAC: RBACConfig{
		Operators:        []string{"31595723"},
		PhysicianSharers: []string{"20111222"},
	}})}

	if !app.physicianCanViewMetrics(PhysicianSummary{Username: "31595723"}) {
		t.Fatalf("operator should be able to view metrics")
	}
	if app.physicianCanViewMetrics(PhysicianSummary{Username: "20111222"}) {
		t.Fatalf("sharer (non-operator) must not view metrics")
	}
	if app.physicianCanViewMetrics(PhysicianSummary{Username: "99999999"}) {
		t.Fatalf("regular physician must not view metrics")
	}
	if (&App{}).physicianCanViewMetrics(PhysicianSummary{Username: "31595723"}) {
		t.Fatalf("nil rbac policy must deny metrics view")
	}
}

func TestNormalizeUsageWindow(t *testing.T) {
	cases := map[string]struct {
		label    string
		duration time.Duration
	}{
		"24h":     {"24h", 24 * time.Hour},
		"7d":      {"7d", 7 * 24 * time.Hour},
		"30d":     {"30d", 30 * 24 * time.Hour},
		"":        {"7d", 7 * 24 * time.Hour},
		"garbage": {"7d", 7 * 24 * time.Hour},
	}
	for in, want := range cases {
		got := normalizeUsageWindow(in)
		if got.label != want.label || got.duration != want.duration {
			t.Fatalf("normalizeUsageWindow(%q) = %+v, want label=%s duration=%s", in, got, want.label, want.duration)
		}
	}
}

// TestRetrieveModalityDim mirrors how the retrieve handlers turn the UI-provided
// `modality` request field into the usage `modality` dim used by the
// "retrieves by modality" breakdown.
func TestRetrieveModalityDim(t *testing.T) {
	cases := map[string]string{
		"":      "UNKNOWN",
		"CT":    "CT",
		"ct":    "CT",
		" mr ":  "MR",
		"CT/MR": "CT/MR",
		"CT/ct": "CT",
	}
	for in, want := range cases {
		if got := usageModalityDim(strings.Split(in, "/")); got != want {
			t.Fatalf("usageModalityDim(split %q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseUsageLimit(t *testing.T) {
	cases := map[string]int{
		"":       usageEventsDefaultLimit,
		"0":      usageEventsDefaultLimit,
		"-5":     usageEventsDefaultLimit,
		"abc":    usageEventsDefaultLimit,
		"50":     50,
		"100000": usageEventsMaxLimit,
	}
	for in, want := range cases {
		if got := parseUsageLimit(in); got != want {
			t.Fatalf("parseUsageLimit(%q) = %d, want %d", in, got, want)
		}
	}
}
