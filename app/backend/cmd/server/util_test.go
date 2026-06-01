package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// These are characterization tests for the pure helpers in util.go.
// They lock in the current, historically bug-prone behavior (DNI validation,
// DICOM/ANDES date and sex normalization, same-origin checks, QIDO parsing,
// rate limiting) before any Phase 2 package extraction. No infra required.

func TestValidateDocumentNumber(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"min length 7", "1234567", false},
		{"max length 11", "12345678901", false},
		{"trimmed to valid", "  1234567  ", false},
		{"too short", "123456", true},
		{"too long", "123456789012", true},
		{"empty", "", true},
		{"non digits", "12345a7", true},
		{"dots not allowed", "12.345.6", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDocumentNumber(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateDocumentNumber(%q) err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestNormalizeProfessionalDocumentInput(t *testing.T) {
	cases := map[string]string{
		" 12.345.678 ": "12345678",
		"12345678":     "12345678",
		"DNI 30111222": "30111222",
		"":             "",
		"abc":          "",
	}
	for in, want := range cases {
		if got := normalizeProfessionalDocumentInput(in); got != want {
			t.Errorf("normalizeProfessionalDocumentInput(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizeDocumentNumberCandidate(t *testing.T) {
	cases := map[string]string{
		" 12.345.678 ": "12345678",
		"30111222":     "30111222",
		"123456":       "", // too short after digitsOnly
		"123456789012": "", // too long
		"abc":          "",
	}
	for in, want := range cases {
		if got := normalizeDocumentNumberCandidate(in); got != want {
			t.Errorf("normalizeDocumentNumberCandidate(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizeRemoteBirthDate(t *testing.T) {
	cases := map[string]string{
		"20200115":    "2020-01-15", // DICOM YYYYMMDD -> ISO
		"2020-01-15":  "2020-01-15", // already ISO, validated
		"2020-13-01":  "2020-13-01", // invalid month -> returned untouched
		"abcdefgh":    "abcdefgh",   // 8 chars but not a date
		"":            "",
		"  20200115 ": "2020-01-15", // trimmed first
	}
	for in, want := range cases {
		if got := normalizeRemoteBirthDate(in); got != want {
			t.Errorf("normalizeRemoteBirthDate(%q)=%q want %q", in, got, want)
		}
	}
}

func TestFormatDICOMDate(t *testing.T) {
	cases := map[string]string{
		"20200115":   "20200115", // valid DICOM stays
		"2020-01-15": "20200115", // ISO -> DICOM
		"2020-13-01": "",         // invalid month
		"abcdefgh":   "",         // not a date
		"":           "",
	}
	for in, want := range cases {
		if got := formatDICOMDate(in); got != want {
			t.Errorf("formatDICOMDate(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizeStudyDate(t *testing.T) {
	cases := map[string]string{
		"20200115":   "2020-01-15",
		"2020-01-15": "2020-01-15", // len 10 -> returned as-is
		"foo":        "foo",
		"":           "",
	}
	for in, want := range cases {
		if got := normalizeStudyDate(in); got != want {
			t.Errorf("normalizeStudyDate(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizeRemoteSex(t *testing.T) {
	cases := map[string]string{
		"F":          "F",
		"femenino":   "F",
		"Female":     "F",
		"mujer":      "F",
		"M":          "M",
		"masculino":  "M",
		"varon":      "M", // accent-insensitive (varón)
		"O":          "O",
		"no binario": "O",
		"x":          "O",
		"unknown":    "UNKNOWN", // default: upper-cased original
		"":           "",
	}
	for in, want := range cases {
		if got := normalizeRemoteSex(in); got != want {
			t.Errorf("normalizeRemoteSex(%q)=%q want %q", in, got, want)
		}
	}
}

func TestJoinProfessionalName(t *testing.T) {
	cases := []struct {
		apellido, nombre, want string
	}{
		{"Doe", "John", "Doe, John"},
		{"Doe", "", "Doe"},
		{"", "John", "John"},
		{"  ", "  ", ""},
		{"  Doe ", " John ", "Doe, John"},
	}
	for _, tc := range cases {
		if got := joinProfessionalName(tc.apellido, tc.nombre); got != tc.want {
			t.Errorf("joinProfessionalName(%q,%q)=%q want %q", tc.apellido, tc.nombre, got, tc.want)
		}
	}
}

func TestStudyUIDFromAccessPath(t *testing.T) {
	cases := []struct {
		path, prefix, want string
	}{
		{"/api/share/1.2.3/access", "/api/share/", "1.2.3"},
		{"/api/share/1.2.3", "/api/share/", ""},               // missing /access
		{"/other/1.2.3/access", "/api/share/", ""},            // missing prefix
		{"/api/share//1.2.3//access", "/api/share/", "1.2.3"}, // surrounding slashes trimmed
	}
	for _, tc := range cases {
		if got := studyUIDFromAccessPath(tc.path, tc.prefix); got != tc.want {
			t.Errorf("studyUIDFromAccessPath(%q,%q)=%q want %q", tc.path, tc.prefix, got, tc.want)
		}
	}
}

func TestStudyUIDFromActionPath(t *testing.T) {
	cases := []struct {
		path, prefix, suffix, want string
	}{
		{"/api/x/1.2.3/go", "/api/x/", "/go", "1.2.3"},
		{"/api/x/1.2.3/go", "/api/x/", "/stop", ""}, // suffix mismatch
		{"/nope/1.2.3/go", "/api/x/", "/go", ""},    // prefix mismatch
	}
	for _, tc := range cases {
		if got := studyUIDFromActionPath(tc.path, tc.prefix, tc.suffix); got != tc.want {
			t.Errorf("studyUIDFromActionPath(%q)=%q want %q", tc.path, got, tc.want)
		}
	}
}

func TestChunkStrings(t *testing.T) {
	got := chunkStrings([]string{"a", "b", "c", "d", "e"}, 2)
	want := [][]string{{"a", "b"}, {"c", "d"}, {"e"}}
	if !equalChunks(got, want) {
		t.Errorf("chunkStrings size 2 = %v want %v", got, want)
	}
	if chunkStrings(nil, 3) != nil {
		t.Errorf("chunkStrings(nil) should be nil")
	}
	// size <= 0 -> single chunk with everything
	one := chunkStrings([]string{"a", "b"}, 0)
	if len(one) != 1 || len(one[0]) != 2 {
		t.Errorf("chunkStrings size 0 = %v want single chunk", one)
	}
}

func TestUniqueTrimmedStrings(t *testing.T) {
	got := uniqueTrimmedStrings([]string{" a ", "a", "b", "", "  ", "b"})
	want := []string{"a", "b"}
	if !equalStrings(got, want) {
		t.Errorf("uniqueTrimmedStrings = %v want %v", got, want)
	}
	if uniqueTrimmedStrings(nil) != nil {
		t.Errorf("uniqueTrimmedStrings(nil) should be nil")
	}
}

func TestMergeStringSets(t *testing.T) {
	// Case-insensitive dedup, preserves first-seen casing and order.
	got := mergeStringSets([]string{"Apple", "apple"}, []string{"Banana", " apple "})
	want := []string{"Apple", "Banana"}
	if !equalStrings(got, want) {
		t.Errorf("mergeStringSets = %v want %v", got, want)
	}
	if len(mergeStringSets()) != 0 {
		t.Errorf("mergeStringSets() should be empty")
	}
}

func TestStartOfCurrentWeek(t *testing.T) {
	// Wednesday 2024-01-03 -> Monday 2024-01-01
	wed := time.Date(2024, time.January, 3, 15, 30, 0, 0, time.UTC)
	if got := startOfCurrentWeek(wed); !got.Equal(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("startOfCurrentWeek(Wed) = %v want 2024-01-01", got)
	}
	// Sunday 2024-01-07 -> Monday 2024-01-01 (week starts Monday)
	sun := time.Date(2024, time.January, 7, 9, 0, 0, 0, time.UTC)
	if got := startOfCurrentWeek(sun); !got.Equal(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("startOfCurrentWeek(Sun) = %v want 2024-01-01", got)
	}
}

func TestCurrentWeekDateRange(t *testing.T) {
	wed := time.Date(2024, time.January, 3, 15, 30, 0, 0, time.UTC)
	start, end := currentWeekDateRange(wed)
	if start != "2024-01-01" || end != "2024-01-07" {
		t.Errorf("currentWeekDateRange(Wed) = (%q,%q) want (2024-01-01,2024-01-07)", start, end)
	}
	sun := time.Date(2024, time.January, 7, 9, 0, 0, 0, time.UTC)
	start, end = currentWeekDateRange(sun)
	if start != "2024-01-01" || end != "2024-01-07" {
		t.Errorf("currentWeekDateRange(Sun) = (%q,%q) want (2024-01-01,2024-01-07)", start, end)
	}
}

func TestFirstForwardedValue(t *testing.T) {
	cases := map[string]string{
		"a, b":  "a",
		" , b":  "b",
		"  c  ": "c",
		"":      "",
		" , , ": "",
	}
	for in, want := range cases {
		if got := firstForwardedValue(in); got != want {
			t.Errorf("firstForwardedValue(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizeOriginHost(t *testing.T) {
	cases := []struct {
		scheme, host, want string
	}{
		{"https", "example.com:443", "example.com"},       // default https port stripped
		{"http", "example.com:80", "example.com"},         // default http port stripped
		{"https", "example.com:8443", "example.com:8443"}, // non-default port kept
		{"https", "example.com", "example.com"},           // no port
		{"https", "", ""},
	}
	for _, tc := range cases {
		if got := normalizeOriginHost(tc.scheme, tc.host); got != tc.want {
			t.Errorf("normalizeOriginHost(%q,%q)=%q want %q", tc.scheme, tc.host, got, tc.want)
		}
	}
}

func TestRequestBaseOrigin(t *testing.T) {
	// Plain HTTP from Host header.
	r := &http.Request{Header: http.Header{}, Host: "example.com:80"}
	if got := requestBaseOrigin(r); got != "http://example.com" {
		t.Errorf("requestBaseOrigin(http) = %q want http://example.com", got)
	}
	// HTTPS via X-Forwarded-Proto + X-Forwarded-Host.
	r2 := &http.Request{Header: http.Header{}, Host: "internal:8080"}
	r2.Header.Set("X-Forwarded-Proto", "https")
	r2.Header.Set("X-Forwarded-Host", "portal.example.com:443")
	if got := requestBaseOrigin(r2); got != "https://portal.example.com" {
		t.Errorf("requestBaseOrigin(https) = %q want https://portal.example.com", got)
	}
	if got := requestBaseOrigin(nil); got != "" {
		t.Errorf("requestBaseOrigin(nil) = %q want empty", got)
	}
}

func TestSameOriginRequest(t *testing.T) {
	makeReq := func(origin string) *http.Request {
		r := &http.Request{Header: http.Header{}, Host: "portal.example.com"}
		r.Header.Set("X-Forwarded-Proto", "https")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	if !sameOriginRequest(makeReq("https://portal.example.com")) {
		t.Errorf("same origin should be true")
	}
	if sameOriginRequest(makeReq("https://evil.example.com")) {
		t.Errorf("different host should be false")
	}
	if sameOriginRequest(makeReq("http://portal.example.com")) {
		t.Errorf("different scheme should be false")
	}
	if sameOriginRequest(makeReq("")) {
		t.Errorf("missing Origin/Referer should be false")
	}
	if sameOriginRequest(nil) {
		t.Errorf("nil request should be false")
	}
}

func TestClientIPForRateLimit(t *testing.T) {
	// CF-Connecting-IP wins.
	r := &http.Request{Header: http.Header{}, RemoteAddr: "10.0.0.1:5555"}
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	r.Header.Set("X-Forwarded-For", "198.51.100.2, 10.0.0.1")
	if got := clientIPForRateLimit(r); got != "203.0.113.7" {
		t.Errorf("CF-Connecting-IP precedence = %q want 203.0.113.7", got)
	}
	// Falls back to first X-Forwarded-For entry.
	r2 := &http.Request{Header: http.Header{}, RemoteAddr: "10.0.0.1:5555"}
	r2.Header.Set("X-Forwarded-For", " 198.51.100.2 , 10.0.0.1 ")
	if got := clientIPForRateLimit(r2); got != "198.51.100.2" {
		t.Errorf("XFF first = %q want 198.51.100.2", got)
	}
	// Falls back to RemoteAddr host.
	r3 := &http.Request{Header: http.Header{}, RemoteAddr: "192.0.2.10:443"}
	if got := clientIPForRateLimit(r3); got != "192.0.2.10" {
		t.Errorf("RemoteAddr host = %q want 192.0.2.10", got)
	}
	if got := clientIPForRateLimit(nil); got != "" {
		t.Errorf("nil request = %q want empty", got)
	}
}

func TestDICOMFirstString(t *testing.T) {
	item := qidoResponseItem{
		"00100020": {Value: []json.RawMessage{json.RawMessage(`"PID-123"`)}},
		"00100010": {Value: []json.RawMessage{json.RawMessage(`{"Alphabetic":"DOE^JOHN"}`)}},
		"empty":    {Value: nil},
	}
	if got := dicomFirstString(item, "00100020"); got != "PID-123" {
		t.Errorf("dicomFirstString plain = %q want PID-123", got)
	}
	if got := dicomFirstString(item, "00100010"); got != "DOE^JOHN" {
		t.Errorf("dicomFirstString PN = %q want DOE^JOHN", got)
	}
	if got := dicomFirstString(item, "missing"); got != "" {
		t.Errorf("dicomFirstString missing = %q want empty", got)
	}
	if got := dicomFirstString(item, "empty"); got != "" {
		t.Errorf("dicomFirstString empty = %q want empty", got)
	}
}

func TestDICOMFirstInt(t *testing.T) {
	item := qidoResponseItem{
		"num":   {Value: []json.RawMessage{json.RawMessage(`"42"`)}},
		"neg":   {Value: []json.RawMessage{json.RawMessage(`"-3"`)}},
		"text":  {Value: []json.RawMessage{json.RawMessage(`"abc"`)}},
		"jsonn": {Value: []json.RawMessage{json.RawMessage(`5`)}}, // JSON number, not a string
	}
	if got := dicomFirstInt(item, "num"); got != 42 {
		t.Errorf("dicomFirstInt num = %d want 42", got)
	}
	if got := dicomFirstInt(item, "neg"); got != 0 {
		t.Errorf("dicomFirstInt neg = %d want 0", got)
	}
	if got := dicomFirstInt(item, "text"); got != 0 {
		t.Errorf("dicomFirstInt text = %d want 0", got)
	}
	// Documents current behavior: a bare JSON number is not parsed as string -> 0.
	if got := dicomFirstInt(item, "jsonn"); got != 0 {
		t.Errorf("dicomFirstInt json-number = %d want 0 (current behavior)", got)
	}
}

func TestDICOMStringList(t *testing.T) {
	item := qidoResponseItem{
		"00080061": {Value: []json.RawMessage{
			json.RawMessage(`"CT"`),
			json.RawMessage(`"  MR "`),
			json.RawMessage(`""`),
			json.RawMessage(`{"Alphabetic":"skip"}`),
		}},
	}
	got := dicomStringList(item, "00080061")
	want := []string{"CT", "MR"}
	if !equalStrings(got, want) {
		t.Errorf("dicomStringList = %v want %v", got, want)
	}
	if dicomStringList(item, "missing") != nil {
		t.Errorf("dicomStringList missing should be nil")
	}
}

func TestExtractPACSSUIDFromRESTMetadata(t *testing.T) {
	entries := []andesRESTPrestacionMetadataEntry{
		{Key: "other", Valor: json.RawMessage(`"x"`)},
		{Key: "PACS-UID", Valor: json.RawMessage(`"1.2.840.10"`)}, // EqualFold match
	}
	if got := extractPACSSUIDFromRESTMetadata(entries); got != "1.2.840.10" {
		t.Errorf("extractPACSSUIDFromRESTMetadata = %q want 1.2.840.10", got)
	}
	none := []andesRESTPrestacionMetadataEntry{{Key: "nope", Valor: json.RawMessage(`"y"`)}}
	if got := extractPACSSUIDFromRESTMetadata(none); got != "" {
		t.Errorf("extractPACSSUIDFromRESTMetadata(no match) = %q want empty", got)
	}
}

func TestInMemoryRateLimiterAllow(t *testing.T) {
	rl := newInMemoryRateLimiter()
	now := time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC)
	window := time.Minute

	if ok, _ := rl.allow("k", 2, window, now); !ok {
		t.Fatalf("1st request should be allowed")
	}
	if ok, _ := rl.allow("k", 2, window, now.Add(time.Second)); !ok {
		t.Fatalf("2nd request should be allowed")
	}
	ok, retryAfter := rl.allow("k", 2, window, now.Add(2*time.Second))
	if ok {
		t.Fatalf("3rd request should be blocked")
	}
	if retryAfter <= 0 {
		t.Fatalf("blocked request should report positive retryAfter, got %v", retryAfter)
	}
	// After the window elapses, requests are allowed again.
	if ok, _ := rl.allow("k", 2, window, now.Add(2*time.Minute)); !ok {
		t.Fatalf("request after window should be allowed")
	}
	// Disabled limiter (limit<=0) and empty key always allow.
	if ok, _ := rl.allow("k", 0, window, now); !ok {
		t.Fatalf("limit<=0 should always allow")
	}
	if ok, _ := rl.allow("", 1, window, now); !ok {
		t.Fatalf("empty key should always allow")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../etc/passwd": "etc-passwd",
		"informe final": "informe-final",
		"":              "informe",
		"   ":           "informe",
		"report_01.pdf": "report_01.pdf",
		"////":          "informe",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q)=%q want %q", in, got, want)
		}
	}
}

func TestDigitsOnly(t *testing.T) {
	cases := map[string]string{
		"a1b2c3":     "123",
		"12.345.678": "12345678",
		"no-digits":  "",
		"":           "",
	}
	for in, want := range cases {
		if got := digitsOnly(in); got != want {
			t.Errorf("digitsOnly(%q)=%q want %q", in, got, want)
		}
	}
}

// --- small comparison helpers (kept local to the test file) ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalChunks(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalStrings(a[i], b[i]) {
			return false
		}
	}
	return true
}
