package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Tier 1 — pure helpers.

func TestJoinURLPath(t *testing.T) {
	cases := []struct {
		base string
		path string
		want string
	}{
		{"https://api.andes.test", "modules/mobileApp/login", "https://api.andes.test/modules/mobileApp/login"},
		{"https://api.andes.test/", "modules/mobileApp/login", "https://api.andes.test/modules/mobileApp/login"},
		{"https://api.andes.test/api", "/modules/mobileApp/login", "https://api.andes.test/api/modules/mobileApp/login"},
	}
	for _, c := range cases {
		got, err := joinURLPath(c.base, c.path)
		if err != nil {
			t.Fatalf("joinURLPath(%q,%q) error: %v", c.base, c.path, err)
		}
		if got != c.want {
			t.Errorf("joinURLPath(%q,%q) = %q want %q", c.base, c.path, got, c.want)
		}
	}

	if _, err := joinURLPath("", "x"); err == nil {
		t.Error("joinURLPath empty base: expected error")
	}
	if _, err := joinURLPath("not-a-url", "x"); err == nil {
		t.Error("joinURLPath scheme-less base: expected error")
	}
}

func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return header + "." + payload + ".sig"
}

func TestDocumentoFromJWT(t *testing.T) {
	if got := documentoFromJWT(makeJWT(t, map[string]any{"documento": "30111222"})); got != "30111222" {
		t.Errorf("top-level documento = %q want 30111222", got)
	}
	nested := makeJWT(t, map[string]any{"usuario": map[string]any{"documento": "40555666"}})
	if got := documentoFromJWT(nested); got != "40555666" {
		t.Errorf("nested documento = %q want 40555666", got)
	}
	if got := documentoFromJWT("not.a.jwt"); got != "" {
		t.Errorf("garbage jwt = %q want empty", got)
	}
	if got := documentoFromJWT(""); got != "" {
		t.Errorf("empty jwt = %q want empty", got)
	}
}

// Tier 3 — Andes login delegation via httptest.

func TestAndesPatientLogin_Success_UserDocumento(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	var gotBody struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"token":"t","user":{"documento":"30111222"}}`)
	}))
	defer srv.Close()

	got, err := andesPatientLogin(context.Background(), srv.Client(), srv.URL, "  Juan@Example.com ", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DocumentNumber != "30111222" {
		t.Errorf("DocumentNumber = %q want 30111222", got.DocumentNumber)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q want POST", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/modules/mobileApp/login") {
		t.Errorf("path = %q want suffix /modules/mobileApp/login", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q want application/json", gotContentType)
	}
	if gotBody.Email != "Juan@Example.com" {
		t.Errorf("email sent = %q want trimmed Juan@Example.com", gotBody.Email)
	}
	if gotBody.Password != "secret" {
		t.Errorf("password sent = %q want secret", gotBody.Password)
	}
}

func TestAndesPatientLogin_Success_JWTFallback(t *testing.T) {
	token := makeJWT(t, map[string]any{"documento": "40555666"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"token":"`+token+`","user":{}}`)
	}))
	defer srv.Close()

	got, err := andesPatientLogin(context.Background(), srv.Client(), srv.URL, "a@b.com", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DocumentNumber != "40555666" {
		t.Errorf("DocumentNumber = %q want 40555666 (from JWT)", got.DocumentNumber)
	}
}

func TestAndesPatientLogin_InvalidCredentials(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"message":"nope"}`)
		}))
		_, err := andesPatientLogin(context.Background(), srv.Client(), srv.URL, "a@b.com", "bad")
		srv.Close()
		if !errors.Is(err, ErrPatientInvalidCredentials) {
			t.Errorf("status %d: err = %v want ErrPatientInvalidCredentials", status, err)
		}
	}
}

func TestAndesPatientLogin_AccountActionRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"new_password_needed":true}`)
	}))
	defer srv.Close()

	_, err := andesPatientLogin(context.Background(), srv.Client(), srv.URL, "a@b.com", "secret")
	if !errors.Is(err, ErrPatientAccountActionRequired) {
		t.Errorf("err = %v want ErrPatientAccountActionRequired", err)
	}
}

func TestAndesPatientLogin_ProviderUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := andesPatientLogin(context.Background(), srv.Client(), srv.URL, "a@b.com", "secret")
	if !errors.Is(err, ErrPatientAuthUnavailable) {
		t.Errorf("err = %v want ErrPatientAuthUnavailable", err)
	}
}

func TestAndesPatientLogin_MissingDocumento(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"token":"","user":{}}`)
	}))
	defer srv.Close()

	_, err := andesPatientLogin(context.Background(), srv.Client(), srv.URL, "a@b.com", "secret")
	if !errors.Is(err, ErrPatientAuthUnavailable) {
		t.Errorf("err = %v want ErrPatientAuthUnavailable", err)
	}
}

func TestAndesPatientLogin_EmptyCredentials(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	if _, err := andesPatientLogin(context.Background(), srv.Client(), srv.URL, "  ", "secret"); !errors.Is(err, ErrPatientInvalidCredentials) {
		t.Errorf("empty email: err = %v want ErrPatientInvalidCredentials", err)
	}
	if _, err := andesPatientLogin(context.Background(), srv.Client(), srv.URL, "a@b.com", ""); !errors.Is(err, ErrPatientInvalidCredentials) {
		t.Errorf("empty password: err = %v want ErrPatientInvalidCredentials", err)
	}
	if called {
		t.Error("server should not be called for empty credentials")
	}
}

func TestValidateExternalConfig_PasswordLoginRequiresAndesBaseURL(t *testing.T) {
	base := ExternalConfig{
		Portal: PortalConfig{
			SessionTimeoutMinutes:            10,
			RetrieveProgressPollSeconds:      2,
			RetrieveWorkerConcurrency:        1,
			ScheduledRetrieveIntervalMinutes: 30,
			ScheduledRetrieveMaxStudyAgeDays: 30,
			ScheduledRetrieveBatchSize:       10,
		},
		Patient: PatientConfig{AuthMode: PatientAuthModeMail, PasswordLoginEnabled: true},
	}

	t.Run("missing base URL fails", func(t *testing.T) {
		t.Setenv("ANDES_MOBILE_API_BASE_URL", "")
		err := validateExternalConfig(base)
		if err == nil || !strings.Contains(err.Error(), "ANDES_MOBILE_API_BASE_URL") {
			t.Fatalf("err = %v want ANDES_MOBILE_API_BASE_URL requirement", err)
		}
	})

	t.Run("with base URL passes patient block", func(t *testing.T) {
		t.Setenv("ANDES_MOBILE_API_BASE_URL", "https://api.andes.test")
		err := validateExternalConfig(base)
		// The patient/password check now passes; validation advances to the
		// next requirement (at least one PACS node), proving the branch cleared.
		if err == nil || strings.Contains(err.Error(), "ANDES_MOBILE_API_BASE_URL") {
			t.Fatalf("err = %v want to advance past the ANDES check", err)
		}
	})
}
