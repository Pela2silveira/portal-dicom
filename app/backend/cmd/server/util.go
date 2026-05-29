package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type InMemoryRateLimiter struct {
	mu        sync.Mutex
	entries   map[string][]time.Time
	lastSweep time.Time
}

type LoginRateLimitRule struct {
	Scope  string
	Limit  int
	Window time.Duration
}

type LoginRateLimitPolicy struct {
	Endpoint string
	Rules    []LoginRateLimitRule
}

var ErrProfessionalNotLicensed = errors.New("professional not licensed")
var ErrProfessionalInvalidCredentials = errors.New("professional invalid credentials")

func activeProfessionalLicenseNumber(doc MongoProfesionalDocument) string {
	for _, formacion := range doc.FormacionGrado {
		for _, matriculacion := range formacion.Matriculacion {
			if !mongoValueIsNull(matriculacion.Baja.Fecha) {
				continue
			}
			licenseNumber := normalizeMongoDocumento(matriculacion.MatriculaNumero)
			if strings.TrimSpace(licenseNumber) != "" {
				return licenseNumber
			}
		}
	}
	return ""
}

func normalizeExceptionSet(values []string) map[string]struct{} {
	normalized := make(map[string]struct{})
	for _, value := range values {
		key := digitsOnly(strings.TrimSpace(value))
		if key == "" {
			key = strings.TrimSpace(value)
		}
		if key == "" {
			continue
		}
		normalized[key] = struct{}{}
	}
	return normalized
}

func chunkStrings(values []string, size int) [][]string {
	if len(values) == 0 {
		return nil
	}
	if size <= 0 {
		size = len(values)
	}
	chunks := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

func extractPACSSUIDFromRESTMetadata(entries []andesRESTPrestacionMetadataEntry) string {
	for _, entry := range entries {
		if !strings.EqualFold(strings.TrimSpace(entry.Key), "pacs-uid") {
			continue
		}
		return jsonRawMessageToString(entry.Valor)
	}
	return ""
}

func jsonRawMessageToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	return strings.TrimSpace(string(raw))
}

func joinProfessionalName(apellido, nombre string) string {
	apellido = strings.TrimSpace(apellido)
	nombre = strings.TrimSpace(nombre)
	switch {
	case apellido != "" && nombre != "":
		return apellido + ", " + nombre
	case apellido != "":
		return apellido
	default:
		return nombre
	}
}

type dicomJSONAttribute struct {
	Value []json.RawMessage `json:"Value"`
}

type PACSNodeResponse struct {
	ID                  string                     `json:"id"`
	Name                string                     `json:"name"`
	AndesOrganizationID string                     `json:"andes_organization_id,omitempty"`
	HIS                 bool                       `json:"his,omitempty"`
	TipoPrestacion      []PACSTipoPrestacionConfig `json:"tipoPrestacion,omitempty"`
	Protocol            string                     `json:"protocol"`
	Priority            int                        `json:"priority"`
	AET                 string                     `json:"aet"`
	DICOMHost           string                     `json:"dicom_host"`
	DICOMPort           int                        `json:"dicom_port"`
	DICOMwebBaseURL     string                     `json:"dicomweb_base_url"`
	SupportsCMove       bool                       `json:"supports_cmove"`
	SupportsCGet        bool                       `json:"supports_cget"`
	Auth                PACSAuthResponse           `json:"auth"`
	Search              PACSNodeSearchResponse     `json:"search"`
	Retrieve            PACSNodeRetrieveResponse   `json:"retrieve"`
	Health              PACSNodeHealthResponse     `json:"health"`
}

type StudyQuery struct {
	PatientID   string
	PatientName string
	DateFrom    string
	DateTo      string
	Modalities  []string
}

type PACSAuthResponse struct {
	Type                string `json:"type"`
	TokenURL            string `json:"token_url"`
	ClientIDEnv         string `json:"client_id_env"`
	ClientSecretEnv     string `json:"client_secret_env"`
	ClientIDPresent     bool   `json:"client_id_present"`
	ClientSecretPresent bool   `json:"client_secret_present"`
}

func studyUIDFromAccessPath(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, "/access") {
		return ""
	}
	value := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/access")
	return strings.Trim(strings.TrimSpace(value), "/")
}

func studyUIDFromActionPath(path, prefix, suffix string) string {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return ""
	}
	value := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return strings.Trim(strings.TrimSpace(value), "/")
}

func lenPACSNodes(cfg *ExternalConfig) int {
	if cfg == nil {
		return 0
	}
	return len(cfg.PACSNodes)
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func newInMemoryRateLimiter() *InMemoryRateLimiter {
	return &InMemoryRateLimiter{
		entries: make(map[string][]time.Time),
	}
}

func (l *InMemoryRateLimiter) allow(key string, limit int, window time.Duration, now time.Time) (bool, time.Duration) {
	if l == nil || strings.TrimSpace(key) == "" || limit <= 0 || window <= 0 {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-window)
	timestamps := l.entries[key]
	kept := timestamps[:0]
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}

	if len(kept) >= limit {
		retryAfter := kept[0].Add(window).Sub(now)
		if retryAfter < 0 {
			retryAfter = 0
		}
		l.entries[key] = kept
		l.sweepLocked(now)
		return false, retryAfter
	}

	kept = append(kept, now)
	l.entries[key] = kept
	l.sweepLocked(now)
	return true, 0
}

func (l *InMemoryRateLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < 5*time.Minute {
		return
	}
	l.lastSweep = now
	cutoff := now.Add(-15 * time.Minute)
	for key, timestamps := range l.entries {
		keepAny := false
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				keepAny = true
				break
			}
		}
		if !keepAny {
			delete(l.entries, key)
		}
	}
}

func clientIPForRateLimit(r *http.Request) string {
	if r == nil {
		return ""
	}

	if forwarded := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); forwarded != "" {
		return forwarded
	}

	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		for _, part := range parts {
			candidate := strings.TrimSpace(part)
			if candidate != "" {
				return candidate
			}
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func requestIsSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(firstForwardedValue(r.Header.Get("X-Forwarded-Proto")), "https")
}

func firstForwardedValue(raw string) string {
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeOriginHost(scheme, rawHost string) string {
	host := strings.TrimSpace(rawHost)
	if host == "" {
		return ""
	}
	lowerScheme := strings.ToLower(strings.TrimSpace(scheme))
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		if (lowerScheme == "https" && parsedPort == "443") || (lowerScheme == "http" && parsedPort == "80") {
			return parsedHost
		}
	}
	return host
}

func requestBaseOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if requestIsSecure(r) {
		scheme = "https"
	}
	host := firstForwardedValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		host = strings.TrimSpace(r.Header.Get("Host"))
	}
	if host == "" {
		return ""
	}
	host = normalizeOriginHost(scheme, host)
	return scheme + "://" + host
}

func requestAbsoluteURL(r *http.Request, path string) string {
	baseOrigin := requestBaseOrigin(r)
	if baseOrigin == "" {
		return path
	}
	return strings.TrimRight(baseOrigin, "/") + path
}

func sameOriginRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	baseOrigin := requestBaseOrigin(r)
	if baseOrigin == "" {
		return false
	}
	baseURL, err := url.Parse(baseOrigin)
	if err != nil {
		return false
	}

	candidates := []string{
		strings.TrimSpace(r.Header.Get("Origin")),
		strings.TrimSpace(r.Referer()),
	}
	for _, rawValue := range candidates {
		if rawValue == "" {
			continue
		}
		parsed, err := url.Parse(rawValue)
		if err != nil {
			continue
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		parsedHost := normalizeOriginHost(parsed.Scheme, parsed.Host)
		baseHost := normalizeOriginHost(baseURL.Scheme, baseURL.Host)
		if strings.EqualFold(parsed.Scheme, baseURL.Scheme) && strings.EqualFold(parsedHost, baseHost) {
			return true
		}
	}

	return false
}

func setPortalSessionCookie(w http.ResponseWriter, r *http.Request, cookieName, token string, expiresAt time.Time) {
	if w == nil || strings.TrimSpace(cookieName) == "" || strings.TrimSpace(token) == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
		Expires:  expiresAt.UTC(),
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func clearPortalSessionCookie(w http.ResponseWriter, r *http.Request, cookieName string) {
	if w == nil || strings.TrimSpace(cookieName) == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	})
}

func uniqueTrimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validateDocumentNumber(value string) error {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 7 || len(trimmed) > 11 {
		return fmt.Errorf("document must contain between 7 and 11 digits")
	}
	for _, r := range trimmed {
		if !unicode.IsDigit(r) {
			return fmt.Errorf("document must contain digits only")
		}
	}
	return nil
}

func normalizeProfessionalDocumentInput(value string) string {
	return digitsOnly(strings.TrimSpace(value))
}

func normalizeRemoteBirthDate(value string) string {
	trimmed := strings.TrimSpace(value)
	switch {
	case len(trimmed) == 8 && !strings.Contains(trimmed, "-"):
		if parsed, err := time.Parse("20060102", trimmed); err == nil {
			return parsed.Format("2006-01-02")
		}
	case len(trimmed) == 10:
		if parsed, err := time.Parse("2006-01-02", trimmed); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return trimmed
}

func formatDICOMDate(value string) string {
	trimmed := strings.TrimSpace(value)
	switch {
	case len(trimmed) == 8 && !strings.Contains(trimmed, "-"):
		if _, err := time.Parse("20060102", trimmed); err == nil {
			return trimmed
		}
	case len(trimmed) == 10:
		if parsed, err := time.Parse("2006-01-02", trimmed); err == nil {
			return parsed.Format("20060102")
		}
	}
	return ""
}

func normalizeRemoteSex(value string) string {
	normalized := normalizeFuzzySearchText(value)
	switch normalized {
	case "F", "FEMENINO", "FEMALE", "MUJER":
		return "F"
	case "M", "MASCULINO", "MALE", "VARON", "HOMBRE":
		return "M"
	case "O", "OTRO", "OTRA", "OTHER", "NO BINARIO", "NO BINARIA", "NB", "X":
		return "O"
	default:
		return strings.ToUpper(strings.TrimSpace(value))
	}
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw + "ms")
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func intFromEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func documentNumberOrFallback(documentNumber, fallback string) string {
	if strings.TrimSpace(documentNumber) != "" {
		return strings.TrimSpace(documentNumber)
	}
	return strings.TrimSpace(fallback)
}

func startOfCurrentWeek(now time.Time) time.Time {
	year, month, day := now.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	weekday := int(start.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return start.AddDate(0, 0, -(weekday - 1))
}

func isTimeoutLikeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// Some wrapped errors preserve timeout only as text.
	return strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded")
}

func mustMarshalJSON(value string) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func dicomFirstString(item qidoResponseItem, tag string) string {
	attribute, ok := item[tag]
	if !ok || len(attribute.Value) == 0 {
		return ""
	}

	var direct string
	if err := json.Unmarshal(attribute.Value[0], &direct); err == nil {
		return strings.TrimSpace(direct)
	}

	var named struct {
		Alphabetic string `json:"Alphabetic"`
	}
	if err := json.Unmarshal(attribute.Value[0], &named); err == nil {
		return strings.TrimSpace(named.Alphabetic)
	}

	return ""
}

func dicomFirstPersonName(item qidoResponseItem, tag string) string {
	return dicomFirstString(item, tag)
}

func dicomFirstInt(item qidoResponseItem, tag string) int {
	value := dicomFirstString(item, tag)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func normalizeStudyDate(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) == 8 && !strings.Contains(trimmed, "-") {
		if parsed, err := time.Parse("20060102", trimmed); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return trimmed
}

func dicomStringList(item qidoResponseItem, tag string) []string {
	attribute, ok := item[tag]
	if !ok || len(attribute.Value) == 0 {
		return nil
	}

	values := make([]string, 0, len(attribute.Value))
	for _, raw := range attribute.Value {
		var direct string
		if err := json.Unmarshal(raw, &direct); err == nil {
			direct = strings.TrimSpace(direct)
			if direct != "" {
				values = append(values, direct)
			}
		}
	}

	return values
}

func currentWeekDateRange(now time.Time) (string, string) {
	year, month, day := now.Date()
	current := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	offset := (int(current.Weekday()) + 6) % 7
	start := current.AddDate(0, 0, -offset)
	end := start.AddDate(0, 0, 6)
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

func mergeStringSets(values ...[]string) []string {
	merged := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range values {
		for _, value := range group {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			key := strings.ToLower(trimmed)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, trimmed)
		}
	}
	return merged
}

func normalizeDocumentNumberCandidate(value string) string {
	digits := strings.TrimSpace(digitsOnly(value))
	if len(digits) < 7 || len(digits) > 11 {
		return ""
	}
	return digits
}

func extractBase64Candidates(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case map[string]any:
		prioritized := []string{"data", "base64", "archivo", "file", "informe", "pdf", "payload"}
		out := make([]string, 0, len(v))
		for _, key := range prioritized {
			raw, ok := v[key]
			if !ok {
				continue
			}
			out = append(out, extractBase64Candidates(raw)...)
		}
		for _, raw := range v {
			out = append(out, extractBase64Candidates(raw)...)
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, extractBase64Candidates(item)...)
		}
		return out
	default:
		return nil
	}
}

func sanitizeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "informe"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	clean := strings.Trim(b.String(), "-_.")
	if clean == "" {
		return "informe"
	}
	return clean
}

func digitsOnly(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func nullIfBlank(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
