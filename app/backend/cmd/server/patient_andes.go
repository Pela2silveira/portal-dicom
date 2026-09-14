package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// patientAndesLoginPath is the Andes mobile API endpoint that authenticates a
// citizen with their Mi Salud account (the same credentials used by the Andes
// mobile app). It returns a JWT plus the user identity.
const patientAndesLoginPath = "modules/mobileApp/login"

// andesPatientLoginResult carries only what the portal needs from the Andes
// login response: the document number used to resolve the patient. The Andes
// JWT and its shiro-trie permissions are intentionally discarded here — the
// portal mints its own patient session and does not rely on Andes claims.
type andesPatientLoginResult struct {
	DocumentNumber string
}

// authenticatePatientAndes delegates patient email/password authentication to
// the Andes mobile API, mirroring the professional LDAP delegation seam. The
// base URL is read from the environment so credentials/endpoints never live in
// tracked config. Returns the resolved document number on success.
func (a *App) authenticatePatientAndes(ctx context.Context, email, password string) (andesPatientLoginResult, error) {
	baseURL := strings.TrimSpace(os.Getenv("ANDES_MOBILE_API_BASE_URL"))
	if baseURL == "" {
		return andesPatientLoginResult{}, fmt.Errorf("%w: missing ANDES_MOBILE_API_BASE_URL", ErrPatientAuthUnavailable)
	}
	client := http.DefaultClient
	if a != nil && a.httpClient != nil {
		client = a.httpClient
	}
	return andesPatientLogin(ctx, client, baseURL, email, password)
}

// andesPatientLogin performs the server-to-server login against the Andes
// mobile API. It is split from authenticatePatientAndes so it can be exercised
// with an httptest server (base URL + client injected).
func andesPatientLogin(ctx context.Context, client *http.Client, baseURL, email, password string) (andesPatientLoginResult, error) {
	email = strings.TrimSpace(email)
	if email == "" || strings.TrimSpace(password) == "" {
		return andesPatientLoginResult{}, ErrPatientInvalidCredentials
	}

	endpoint, err := joinURLPath(baseURL, patientAndesLoginPath)
	if err != nil {
		return andesPatientLoginResult{}, fmt.Errorf("%w: %v", ErrPatientAuthUnavailable, err)
	}

	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return andesPatientLoginResult{}, fmt.Errorf("%w: %v", ErrPatientAuthUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return andesPatientLoginResult{}, fmt.Errorf("%w: %v", ErrPatientAuthUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return andesPatientLoginResult{}, fmt.Errorf("%w: %v", ErrPatientAuthUnavailable, err)
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	switch res.StatusCode {
	case http.StatusOK:
		// fallthrough to body parsing below
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest:
		return andesPatientLoginResult{}, ErrPatientInvalidCredentials
	default:
		return andesPatientLoginResult{}, fmt.Errorf("%w: unexpected status %d", ErrPatientAuthUnavailable, res.StatusCode)
	}

	var payload struct {
		Token string `json:"token"`
		User  struct {
			Documento string `json:"documento"`
		} `json:"user"`
		// Some Andes account states are signalled on a 200 response (account
		// not yet activated, forced password reset). Treat them as a distinct
		// remediation case so the UI can point the citizen at Mi Salud.
		NewPasswordNeeded bool `json:"new_password_needed"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return andesPatientLoginResult{}, fmt.Errorf("%w: decode login response: %v", ErrPatientAuthUnavailable, err)
	}

	if payload.NewPasswordNeeded {
		return andesPatientLoginResult{}, ErrPatientAccountActionRequired
	}

	doc := strings.TrimSpace(payload.User.Documento)
	if doc == "" {
		doc = documentoFromJWT(payload.Token)
	}
	if doc == "" {
		return andesPatientLoginResult{}, fmt.Errorf("%w: login response missing documento", ErrPatientAuthUnavailable)
	}

	return andesPatientLoginResult{DocumentNumber: doc}, nil
}

// documentoFromJWT extracts a document number from the JWT payload as a
// fallback when the login response body does not carry user.documento. The
// signature is NOT verified: this is a server-to-server call over TLS and the
// value is only used to look up an existing patient record (never to grant
// Andes-scoped permissions).
func documentoFromJWT(token string) string {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some encoders emit padded segments.
		decoded, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}

	var claims struct {
		Documento string `json:"documento"`
		Usuario   struct {
			Documento string `json:"documento"`
		} `json:"usuario"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	if d := strings.TrimSpace(claims.Documento); d != "" {
		return d
	}
	return strings.TrimSpace(claims.Usuario.Documento)
}

// joinURLPath joins a base URL with a relative path, tolerating a trailing
// slash on the base and a leading slash on the path.
func joinURLPath(baseURL, p string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("empty base URL")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", baseURL)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(p, "/")
	return u.String(), nil
}
