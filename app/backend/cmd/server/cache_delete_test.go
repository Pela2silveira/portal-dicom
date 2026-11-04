package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStudyCacheDeletePermission(t *testing.T) {
	policy := buildRBACPolicy(nil)

	if !policy.hasPermission(RoleOperator, "study:cache_delete") {
		t.Fatalf("operator (wildcard) must be granted study:cache_delete")
	}
	for _, role := range []string{RolePhysician, RolePhysicianSharer, RolePatient, RolePublic} {
		if policy.hasPermission(role, "study:cache_delete") {
			t.Fatalf("role %q must NOT be granted study:cache_delete", role)
		}
	}
}

func TestPhysicianCanDeleteCache(t *testing.T) {
	app := &App{rbac: buildRBACPolicy(&ExternalConfig{RBAC: RBACConfig{
		Operators:        []string{"31595723"},
		PhysicianSharers: []string{"20111222"},
	}})}

	if !app.physicianCanDeleteCache(PhysicianSummary{Username: "31595723"}) {
		t.Fatalf("operator should be able to delete a cached study")
	}
	if app.physicianCanDeleteCache(PhysicianSummary{DNI: "20111222"}) {
		t.Fatalf("sharer must not delete a cached study")
	}
	if app.physicianCanDeleteCache(PhysicianSummary{Username: "99999999"}) {
		t.Fatalf("regular physician must not delete a cached study")
	}
	if (&App{}).physicianCanDeleteCache(PhysicianSummary{Username: "31595723"}) {
		t.Fatalf("nil rbac policy must deny cache delete")
	}
}

func TestOrthancDeleteAccepted(t *testing.T) {
	if !orthancDeleteAccepted(http.StatusOK) || !orthancDeleteAccepted(http.StatusNotFound) {
		t.Fatalf("200 and 404 must count as a removed study")
	}
	if orthancDeleteAccepted(http.StatusForbidden) || orthancDeleteAccepted(http.StatusInternalServerError) {
		t.Fatalf("403 and 500 must not count as a removed study")
	}
}

func TestRetrieveJobInFlight(t *testing.T) {
	now := time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)
	created := now.Add(-time.Minute)
	started := now.Add(-30 * time.Second)

	if !retrieveJobInFlight("running", created, started, true, false, now) {
		t.Fatalf("a fresh running job must block cache delete")
	}
	if !retrieveJobInFlight("queued", created, time.Time{}, false, false, now) {
		t.Fatalf("a fresh queued job must block cache delete")
	}
	if retrieveJobInFlight("running", created, started, true, true, now) {
		t.Fatalf("a finished job must not block cache delete")
	}
	if retrieveJobInFlight("done", created, started, true, false, now) {
		t.Fatalf("a done job must not block cache delete")
	}
	if retrieveJobInFlight("failed", created, started, true, false, now) {
		t.Fatalf("a failed job must not block cache delete")
	}
	stale := now.Add(-retrieveJobStaleAfter - time.Second)
	if retrieveJobInFlight("running", stale, time.Time{}, false, false, now) {
		t.Fatalf("a stale running job must not block cache delete")
	}
}

func TestDeleteOrthancStudy(t *testing.T) {
	var gotMethod, gotPath, gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotToken = r.Header.Get(orthancInternalTokenHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	app := &App{
		cfg:        Config{OrthancURL: server.URL, OrthancInternalToken: "internal-token"},
		httpClient: server.Client(),
		logger:     discardLogger(),
	}
	if err := app.deleteOrthancStudy(context.Background(), "d65ad728-b47d8b90-db0548fd-120a17b7-9fe00ad0"); err != nil {
		t.Fatalf("deleteOrthancStudy: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/studies/d65ad728-b47d8b90-db0548fd-120a17b7-9fe00ad0" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotToken != "internal-token" {
		t.Fatalf("internal token header = %q", gotToken)
	}
}

func TestDeleteOrthancStudyNotFoundIsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	app := &App{
		cfg:        Config{OrthancURL: server.URL},
		httpClient: server.Client(),
		logger:     discardLogger(),
	}
	if err := app.deleteOrthancStudy(context.Background(), "abc"); err != nil {
		t.Fatalf("404 must be treated as already deleted, got %v", err)
	}
}

func TestDeleteOrthancStudyRejectsForbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()

	app := &App{
		cfg:        Config{OrthancURL: server.URL},
		httpClient: server.Client(),
		logger:     discardLogger(),
	}
	err := app.deleteOrthancStudy(context.Background(), "abc")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("403 must fail the delete, got %v", err)
	}
}

func TestDeleteOrthancStudyRequiresOrthancID(t *testing.T) {
	app := &App{cfg: Config{OrthancURL: "http://orthanc:8042"}, logger: discardLogger()}
	if err := app.deleteOrthancStudy(context.Background(), "  "); err == nil {
		t.Fatalf("empty orthanc id must be rejected")
	}
}

func TestHandlePhysicianStudyCacheDeleteMethodAndPath(t *testing.T) {
	app := &App{logger: discardLogger()}

	getReq := httptest.NewRequest(http.MethodGet, "/api/physician/studies/1.2.3/cache", nil)
	getRec := httptest.NewRecorder()
	app.handlePhysicianStudyCacheDelete(getRec, getReq)
	if getRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", getRec.Code)
	}

	missingReq := httptest.NewRequest(http.MethodDelete, "/api/physician/studies//cache", nil)
	missingRec := httptest.NewRecorder()
	app.handlePhysicianStudyCacheDelete(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("empty uid status = %d, want 404", missingRec.Code)
	}
}
