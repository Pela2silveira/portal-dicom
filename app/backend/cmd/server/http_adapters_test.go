package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Tier 3 — HTTP adapters via httptest. Outbound clients are pointed at an
// in-process httptest server (no real Orthanc/ANDES, no network); inbound
// middleware is exercised with httptest.NewRecorder. No DB involved.

func newAndesRESTSource(client *http.Client, baseURL, token string) *AndesRESTPrestacionLookupSource {
	return &AndesRESTPrestacionLookupSource{
		httpClient: client,
		baseURL:    baseURL,
		token:      token,
		timeout:    2 * time.Second,
	}
}

func TestAndesREST_FindByPatientMongoID_RequestAndParsing(t *testing.T) {
	var gotAuth, gotAccept, gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"p1","paciente":{"id":"MID"},
			 "metadata":[{"key":"pacs-uid","valor":"1.2.3"}],
			 "solicitud":{"tipoPrestacion":{"fsn":"","term":"TAC de cerebro"},
			              "profesional":{"apellido":"Perez","nombre":"Juan"}}},
			{"id":"p2","paciente":{"id":"MID"},
			 "metadata":[{"key":"pacs-uid","valor":"1.2.3"}],
			 "solicitud":{"tipoPrestacion":{"fsn":"DUP"}}},
			{"id":"p3","paciente":{"id":"OTHER"},
			 "metadata":[{"key":"pacs-uid","valor":"9.9.9"}]}
		]`)
	}))
	defer srv.Close()

	src := newAndesRESTSource(srv.Client(), srv.URL, "TKN")
	got, err := src.FindByPatientMongoID(context.Background(), "MID", []string{"C1", "C2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Outbound request shape.
	if gotAuth != "JWT TKN" {
		t.Errorf("Authorization = %q want %q (ANDES uses JWT, not Bearer)", gotAuth, "JWT TKN")
	}
	if !strings.HasSuffix(gotPath, "/modules/rup/prestaciones") {
		t.Errorf("path = %q want suffix /modules/rup/prestaciones", gotPath)
	}
	if gotQuery.Get("idPaciente") != "MID" {
		t.Errorf("idPaciente = %q want MID", gotQuery.Get("idPaciente"))
	}
	if gotQuery.Get("estado") != "validada" {
		t.Errorf("estado = %q want validada", gotQuery.Get("estado"))
	}
	if tp := gotQuery["tipoPrestaciones"]; !equalStrings(tp, []string{"C1", "C2"}) {
		t.Errorf("tipoPrestaciones = %v want [C1 C2]", tp)
	}
	if !strings.Contains(gotAccept, "application/json") {
		t.Errorf("Accept = %q want application/json", gotAccept)
	}

	// Response parsing: dedup by pacs-uid, filter by paciente.id, FSN->Term fallback, name join.
	if len(got) != 1 {
		t.Fatalf("results = %v want exactly 1 (dedup + other-patient filtered)", got)
	}
	sum, ok := got["1.2.3"]
	if !ok {
		t.Fatalf("missing summary for 1.2.3; got %v", got)
	}
	if sum.PrestacionID != "p1" {
		t.Errorf("PrestacionID = %q want p1", sum.PrestacionID)
	}
	if sum.PrestacionFSN != "TAC de cerebro" {
		t.Errorf("PrestacionFSN = %q want TAC de cerebro (Term fallback)", sum.PrestacionFSN)
	}
	if sum.Professional != "Perez, Juan" {
		t.Errorf("Professional = %q want Perez, Juan", sum.Professional)
	}
	if _, exists := got["9.9.9"]; exists {
		t.Errorf("study for another patient must be filtered out")
	}
}

func TestAndesREST_ErrorPaths(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		src := newAndesRESTSource(http.DefaultClient, "http://unused.invalid", "")
		if _, err := src.FindByPatientMongoID(context.Background(), "MID", nil); err == nil {
			t.Errorf("expected error when token is missing")
		}
	})

	t.Run("empty mongo id short-circuits", func(t *testing.T) {
		src := newAndesRESTSource(http.DefaultClient, "http://unused.invalid", "TKN")
		m, err := src.FindByPatientMongoID(context.Background(), "   ", nil)
		if err != nil || len(m) != 0 {
			t.Errorf("empty mongoID = (%v,%v) want (empty,nil)", m, err)
		}
	})

	t.Run("401 unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		src := newAndesRESTSource(srv.Client(), srv.URL, "TKN")
		if _, err := src.FindByPatientMongoID(context.Background(), "MID", nil); err == nil {
			t.Errorf("expected error on 401")
		}
	})

	t.Run("5xx server error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()
		src := newAndesRESTSource(srv.Client(), srv.URL, "TKN")
		if _, err := src.FindByPatientMongoID(context.Background(), "MID", nil); err == nil {
			t.Errorf("expected error on 500")
		}
	})

	t.Run("invalid json body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `not-json`)
		}))
		defer srv.Close()
		src := newAndesRESTSource(srv.Client(), srv.URL, "TKN")
		if _, err := src.FindByPatientMongoID(context.Background(), "MID", nil); err == nil {
			t.Errorf("expected decode error on invalid json")
		}
	})
}

func TestAndesRESTSource_Metadata(t *testing.T) {
	src := newAndesRESTSource(nil, "http://x", "TKN")
	if src.ProviderName() != "andes_rest" {
		t.Errorf("ProviderName = %q want andes_rest", src.ProviderName())
	}
	if src.Mode() != HISPrestacionesProviderREST {
		t.Errorf("Mode = %q want %q", src.Mode(), HISPrestacionesProviderREST)
	}
	if !src.Healthy() {
		t.Errorf("expected healthy with token+baseURL")
	}
	if newAndesRESTSource(nil, "", "").Healthy() {
		t.Errorf("expected unhealthy without token/baseURL")
	}
	if m, err := src.FindByStudyUIDs(context.Background(), []string{"1.2.3"}); err != nil || len(m) != 0 {
		t.Errorf("REST FindByStudyUIDs is a no-op; got (%v,%v)", m, err)
	}
}

func TestCheckRemotePACSHTTP(t *testing.T) {
	t.Run("healthy on 2xx and hits /studies?limit=1", func(t *testing.T) {
		var gotPath, gotRawQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotRawQuery = r.URL.RawQuery
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		app := &App{httpClient: srv.Client(), logger: discardLogger()}
		resolved := PACSNodeResolvedConfig{ID: "n1", DICOMwebBaseURL: srv.URL}
		if !app.checkRemotePACSHTTP(context.Background(), resolved) {
			t.Errorf("expected healthy on 200")
		}
		if gotPath != "/studies" || gotRawQuery != "limit=1" {
			t.Errorf("probe = %q?%q want /studies?limit=1", gotPath, gotRawQuery)
		}
	})

	t.Run("unhealthy on 5xx", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		app := &App{httpClient: srv.Client(), logger: discardLogger()}
		if app.checkRemotePACSHTTP(context.Background(), PACSNodeResolvedConfig{ID: "n1", DICOMwebBaseURL: srv.URL}) {
			t.Errorf("expected unhealthy on 500")
		}
	})

	t.Run("false on empty base url", func(t *testing.T) {
		app := &App{httpClient: http.DefaultClient, logger: discardLogger()}
		if app.checkRemotePACSHTTP(context.Background(), PACSNodeResolvedConfig{ID: "n1"}) {
			t.Errorf("expected false for empty DICOMwebBaseURL")
		}
	})
}

func TestWithBrowserOriginCheck(t *testing.T) {
	app := &App{logger: discardLogger()}
	build := func() (http.HandlerFunc, *bool) {
		called := false
		h := app.withBrowserOriginCheck(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})
		return h, &called
	}
	httpsReq := func(method, origin string) *http.Request {
		req := httptest.NewRequest(method, "https://portal.example.com/api/x", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		return req
	}

	t.Run("cross-origin POST is rejected with 403", func(t *testing.T) {
		h, called := build()
		rec := httptest.NewRecorder()
		h(rec, httpsReq(http.MethodPost, "https://evil.example.com"))
		if rec.Code != http.StatusForbidden {
			t.Errorf("code = %d want 403", rec.Code)
		}
		if *called {
			t.Errorf("next must not run on rejected cross-origin POST")
		}
	})

	t.Run("same-origin POST passes through", func(t *testing.T) {
		h, called := build()
		rec := httptest.NewRecorder()
		h(rec, httpsReq(http.MethodPost, "https://portal.example.com"))
		if !*called {
			t.Errorf("next must run on same-origin POST")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("code = %d want 200", rec.Code)
		}
	})

	t.Run("GET bypasses the origin check", func(t *testing.T) {
		h, called := build()
		rec := httptest.NewRecorder()
		h(rec, httpsReq(http.MethodGet, "https://evil.example.com"))
		if !*called {
			t.Errorf("GET must bypass the cross-origin check")
		}
	})
}
