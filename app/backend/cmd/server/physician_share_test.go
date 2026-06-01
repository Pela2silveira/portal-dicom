package main

import (
	"strings"
	"testing"
	"time"
)

func TestPhysicianCanShare(t *testing.T) {
	cfg := &ExternalConfig{
		RBAC: RBACConfig{
			PhysicianSharers: []string{"31595723"},
		},
	}
	app := &App{rbac: buildRBACPolicy(cfg)}

	if !app.physicianCanShare(PhysicianSummary{Username: "31595723"}) {
		t.Fatalf("configured sharer (by username/dni) should be able to share")
	}
	if !app.physicianCanShare(PhysicianSummary{DNI: "31595723"}) {
		t.Fatalf("configured sharer (by dni) should be able to share")
	}
	if app.physicianCanShare(PhysicianSummary{Username: "99999999"}) {
		t.Fatalf("non-sharer physician must not be able to share")
	}

	// Nil policy is safe and denies sharing.
	if (&App{}).physicianCanShare(PhysicianSummary{Username: "31595723"}) {
		t.Fatalf("nil rbac policy must deny sharing")
	}
}

// Physician share links are persisted with a NULL owner (patient_id), surfaced
// as an empty PatientID in the snapshot. The consume path must treat an
// ownerless link as valid and redirect to the viewer without depending on a
// patient identity. Regression: a NULL patient_id scanned into a non-nullable
// string broke consumption with a 500 ("failed to load share link"); the query
// now COALESCEs it to "".
func TestPhysicianShareOwnerlessLinkConsumable(t *testing.T) {
	link := studyShareLinkSnapshot{
		ShareID:          "share-1",
		PatientID:        "", // physician share: no owner row
		StudyInstanceUID: "1.2.3.4.5",
		ViewerKind:       "stone",
		Channel:          "share",
		Status:           "active",
		MaxUses:          10,
		ConsumedUses:     0,
		ExpiresAt:        time.Now().UTC().Add(time.Hour),
	}

	if _, denied := validateStudyShareLink(link, time.Now().UTC()); denied {
		t.Fatalf("ownerless physician share link must validate as consumable")
	}

	redirect := studyShareTokenRedirectURL("tok-123", link)
	if !strings.Contains(redirect, link.StudyInstanceUID) {
		t.Fatalf("redirect should target the shared study, got %q", redirect)
	}
	if !strings.Contains(redirect, "tok-123") {
		t.Fatalf("redirect should carry the share token, got %q", redirect)
	}

	// Orthanc study-level validation grants on UID match, independent of owner.
	resp, reason := validateStudyShareLinkForOrthanc(link, orthancTokenValidationRequest{
		Method:   "GET",
		Level:    "study",
		DICOMUID: link.StudyInstanceUID,
	})
	if !resp.Granted {
		t.Fatalf("study-level orthanc validation should grant for matching UID (reason=%q)", reason)
	}
}
