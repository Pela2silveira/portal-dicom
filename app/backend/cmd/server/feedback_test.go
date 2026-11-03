package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeFeedbackMessage(t *testing.T) {
	cases := map[string]string{
		"  hola  ":                  "hola",
		"linea1\nlinea2":            "linea1\nlinea2",
		"con\ttab":                  "con\ttab",
		"control\x00\x07chars":      "controlchars",
		"<script>alert(1)</script>": "<script>alert(1)</script>", // not escaped here; escaped on render
		"":                          "",
		"   ":                       "",
	}
	for in, want := range cases {
		if got := sanitizeFeedbackMessage(in); got != want {
			t.Fatalf("sanitizeFeedbackMessage(%q) = %q, want %q", in, got, want)
		}
	}

	// Length cap (rune-aware).
	long := strings.Repeat("á", feedbackMessageMaxLen+50)
	got := sanitizeFeedbackMessage(long)
	if utf8.RuneCountInString(got) > feedbackMessageMaxLen {
		t.Fatalf("sanitized message exceeds cap: %d runes", utf8.RuneCountInString(got))
	}

	// Invalid UTF-8 is dropped, leaving valid content.
	if got := sanitizeFeedbackMessage("ok\xff\xfe"); got != "ok" {
		t.Fatalf("invalid utf-8 not stripped: %q", got)
	}
}

func TestNormalizeFeedbackStudySite(t *testing.T) {
	if got := normalizeFeedbackStudySite("  Hospital\nCastro   Rendón  "); got != "Hospital Castro Rendón" {
		t.Fatalf("study site = %q", got)
	}
	if got := normalizeFeedbackStudySite("   "); got != "" {
		t.Fatalf("empty study site = %q", got)
	}
	long := strings.Repeat("a", feedbackStudySiteMaxLen+20)
	if utf8.RuneCountInString(normalizeFeedbackStudySite(long)) > feedbackStudySiteMaxLen {
		t.Fatal("study site exceeds cap")
	}
}

func TestNormalizeFeedbackPhone(t *testing.T) {
	got, ok := normalizeFeedbackPhone("  +54 299 555-1234  ")
	if !ok || got != "+54 299 555-1234" {
		t.Fatalf("phone = %q, %v", got, ok)
	}
	if got, ok := normalizeFeedbackPhone("   "); !ok || got != "" {
		t.Fatalf("empty phone = %q, %v", got, ok)
	}
	for _, in := range []string{"123", "abc123456", "299 555 1234 ext 9", strings.Repeat("1", 16)} {
		if _, ok := normalizeFeedbackPhone(in); ok {
			t.Fatalf("phone %q must be rejected", in)
		}
	}
}

func TestParseFeedbackLimit(t *testing.T) {
	cases := map[string]int{
		"":       feedbackDefaultLimit,
		"0":      feedbackDefaultLimit,
		"-3":     feedbackDefaultLimit,
		"abc":    feedbackDefaultLimit,
		"25":     25,
		"100000": feedbackMaxLimit,
	}
	for in, want := range cases {
		if got := parseFeedbackLimit(in); got != want {
			t.Fatalf("parseFeedbackLimit(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFeedbackActionPermissions(t *testing.T) {
	// feedback.create has no RBAC gate but requires a logged-in subject
	// (patient/physician); anonymous visitors (public) are not allowed.
	create := actionCatalog[ActionFeedbackCreate]
	if create.Permission != "" {
		t.Fatalf("feedback.create must have no permission gate, got %q", create.Permission)
	}
	if !create.Mutating {
		t.Fatalf("feedback.create must be mutating (same-origin enforced)")
	}
	for _, kind := range []SubjectKind{SubjectPatient, SubjectPhysician} {
		if !create.allowsSubject(kind) {
			t.Fatalf("feedback.create must allow subject %q", kind)
		}
	}
	if create.allowsSubject(SubjectPublic) {
		t.Fatalf("feedback.create must NOT allow anonymous (public) subject")
	}

	// feedback.read is operator-only (wildcard role), denied to everyone else.
	policy := buildRBACPolicy(&ExternalConfig{})
	if !policy.hasPermission(RoleOperator, "feedback:read") {
		t.Fatalf("operator must be granted feedback:read")
	}
	for _, role := range []string{RolePhysician, RolePhysicianSharer, RolePatient, RolePublic} {
		if policy.hasPermission(role, "feedback:read") {
			t.Fatalf("role %q must NOT be granted feedback:read", role)
		}
	}
}
