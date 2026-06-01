package main

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"
)

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// --- fakes for the identity-source seams ---

type fakePatientSource struct {
	identity PatientIdentity
	err      error
	calls    int
	lastDoc  string
}

func (f *fakePatientSource) ProviderName() string { return "fake-patient" }
func (f *fakePatientSource) ResolveByDocument(_ context.Context, doc string) (PatientIdentity, error) {
	f.calls++
	f.lastDoc = doc
	return f.identity, f.err
}

// closeablePatientSource also satisfies patientIdentitySourceCloser.
type closeablePatientSource struct {
	fakePatientSource
	closed int
}

func (f *closeablePatientSource) Close(_ context.Context) error {
	f.closed++
	return nil
}

type fakeProfessionalSource struct {
	identity ProfessionalIdentity
	err      error
	calls    int
}

func (f *fakeProfessionalSource) ProviderName() string { return "fake-prof" }
func (f *fakeProfessionalSource) ResolveByUsername(_ context.Context, _ string) (ProfessionalIdentity, error) {
	f.calls++
	return f.identity, f.err
}

// --- RetryingPatientIdentitySource ---

func TestRetryingPatientIdentitySource_Delegates(t *testing.T) {
	fake := &fakePatientSource{identity: PatientIdentity{DocumentNumber: "30111222", FullName: "PEREZ, JUAN"}}
	s := NewRetryingPatientIdentitySource("his", discardLogger(), time.Hour, func() (PatientIdentitySource, error) {
		return fake, nil
	})
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	got, err := s.ResolveByDocument(context.Background(), "30111222")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.FullName != "PEREZ, JUAN" {
		t.Errorf("delegated identity = %q want PEREZ, JUAN", got.FullName)
	}
	if fake.calls != 1 || fake.lastDoc != "30111222" {
		t.Errorf("underlying not called once with doc: calls=%d lastDoc=%q", fake.calls, fake.lastDoc)
	}
	if s.ProviderName() != "his" {
		t.Errorf("ProviderName = %q want his", s.ProviderName())
	}
	// Underlying fake does not implement dependencyHealthReporter -> defaults to healthy.
	if !s.Healthy() {
		t.Errorf("expected healthy when underlying has no health reporter")
	}
}

func TestRetryingPatientIdentitySource_BuildFailureFallsBackToUnavailable(t *testing.T) {
	buildErr := errors.New("mongo down")
	s := NewRetryingPatientIdentitySource("his", discardLogger(), time.Hour, func() (PatientIdentitySource, error) {
		return nil, buildErr
	})
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	_, err := s.ResolveByDocument(context.Background(), "x")
	if err == nil {
		t.Fatalf("expected error when provider failed to build")
	}
	if !errors.Is(err, buildErr) {
		t.Errorf("error should wrap build error, got %v", err)
	}
	if s.Healthy() {
		t.Errorf("expected unhealthy after build failure")
	}
}

func TestRetryingPatientIdentitySource_RecoversOnRefresh(t *testing.T) {
	down := true
	fake := &fakePatientSource{identity: PatientIdentity{FullName: "OK"}}
	s := NewRetryingPatientIdentitySource("his", discardLogger(), time.Hour, func() (PatientIdentitySource, error) {
		if down {
			return nil, errors.New("down")
		}
		return fake, nil
	})
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	if _, err := s.ResolveByDocument(context.Background(), "1"); err == nil {
		t.Fatalf("expected error while provider is down")
	}

	down = false
	s.refresh() // simulate a retry tick deterministically

	got, err := s.ResolveByDocument(context.Background(), "1")
	if err != nil {
		t.Fatalf("unexpected error after recovery: %v", err)
	}
	if got.FullName != "OK" {
		t.Errorf("recovered identity = %q want OK", got.FullName)
	}
}

func TestRetryingPatientIdentitySource_CloseClosesUnderlying(t *testing.T) {
	fake := &closeablePatientSource{}
	s := NewRetryingPatientIdentitySource("his", discardLogger(), time.Hour, func() (PatientIdentitySource, error) {
		return fake, nil
	})
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if fake.closed == 0 {
		t.Errorf("underlying Close was not called")
	}
	// Close must be idempotent (stopOnce guards the channel close).
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("second Close returned error: %v", err)
	}
}

// --- RetryingProfessionalIdentitySource ---

func TestRetryingProfessionalIdentitySource_Delegates(t *testing.T) {
	fake := &fakeProfessionalSource{identity: ProfessionalIdentity{Username: "jperez", Licensed: true}}
	s := NewRetryingProfessionalIdentitySource("ldap", discardLogger(), time.Hour, func() (ProfessionalIdentitySource, error) {
		return fake, nil
	})
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	got, err := s.ResolveByUsername(context.Background(), "jperez")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Username != "jperez" || !got.Licensed {
		t.Errorf("delegated identity = %+v", got)
	}
	if fake.calls != 1 {
		t.Errorf("underlying calls = %d want 1", fake.calls)
	}
}

func TestRetryingProfessionalIdentitySource_BuildFailureFallsBack(t *testing.T) {
	buildErr := errors.New("ldap down")
	s := NewRetryingProfessionalIdentitySource("ldap", discardLogger(), time.Hour, func() (ProfessionalIdentitySource, error) {
		return nil, buildErr
	})
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	if _, err := s.ResolveByUsername(context.Background(), "x"); !errors.Is(err, buildErr) {
		t.Errorf("error should wrap build error, got %v", err)
	}
	if s.Healthy() {
		t.Errorf("expected unhealthy after build failure")
	}
}

// --- Unavailable sources ---

func TestUnavailablePatientIdentitySource(t *testing.T) {
	withProvider := &UnavailablePatientIdentitySource{provider: "mongo", err: errors.New("conn refused")}
	if withProvider.ProviderName() != "mongo" {
		t.Errorf("ProviderName = %q want mongo", withProvider.ProviderName())
	}
	if _, err := withProvider.ResolveByDocument(context.Background(), "1"); err == nil {
		t.Errorf("expected error from unavailable source")
	}
	if withProvider.Healthy() {
		t.Errorf("unavailable source must report unhealthy")
	}

	blank := &UnavailablePatientIdentitySource{}
	if blank.ProviderName() != "unavailable" {
		t.Errorf("blank provider name = %q want unavailable", blank.ProviderName())
	}
}

func TestUnavailableProfessionalIdentitySource(t *testing.T) {
	src := &UnavailableProfessionalIdentitySource{}
	if src.ProviderName() != "unavailable" {
		t.Errorf("ProviderName = %q want unavailable", src.ProviderName())
	}
	if _, err := src.ResolveByUsername(context.Background(), "x"); err == nil {
		t.Errorf("expected error from unavailable source")
	}
	if src.Healthy() {
		t.Errorf("unavailable source must report unhealthy")
	}
}

// --- Prestación lookup sources ---

func TestNoopPrestacionLookupSource(t *testing.T) {
	noop := &NoopPrestacionLookupSource{}
	if noop.ProviderName() != "noop" || noop.Mode() != "noop" {
		t.Errorf("noop provider/mode = %q/%q", noop.ProviderName(), noop.Mode())
	}
	if m, err := noop.FindByStudyUIDs(context.Background(), []string{"1.2.3"}); err != nil || len(m) != 0 {
		t.Errorf("FindByStudyUIDs = (%v,%v) want empty,nil", m, err)
	}
	if m, err := noop.FindByPatientMongoID(context.Background(), "id", nil); err != nil || len(m) != 0 {
		t.Errorf("FindByPatientMongoID = (%v,%v) want empty,nil", m, err)
	}
}

func TestCompositePrestacionLookupSource_Routing(t *testing.T) {
	// No backends configured -> everything is a no-op, ProviderName "noop".
	empty := &CompositePrestacionLookupSource{mode: "auto"}
	if empty.Mode() != "auto" {
		t.Errorf("Mode = %q want auto", empty.Mode())
	}
	if empty.ProviderName() != "noop" {
		t.Errorf("ProviderName(no backends) = %q want noop", empty.ProviderName())
	}
	if m, err := empty.FindByStudyUIDs(context.Background(), []string{"1.2.3"}); err != nil || len(m) != 0 {
		t.Errorf("FindByStudyUIDs(no mongo) = (%v,%v) want empty,nil", m, err)
	}
	if m, err := empty.FindByPatientMongoID(context.Background(), "id", nil); err != nil || len(m) != 0 {
		t.Errorf("FindByPatientMongoID(no rest) = (%v,%v) want empty,nil", m, err)
	}
	if err := empty.Close(context.Background()); err != nil {
		t.Errorf("Close(no backends) = %v want nil", err)
	}

	// ProviderName reflects which backends are wired (zero-value pointers are
	// only inspected for nil-ness here, never dereferenced).
	both := &CompositePrestacionLookupSource{rest: &AndesRESTPrestacionLookupSource{}, mongo: &MongoPrestacionLookupSource{}, mode: "auto"}
	if got := both.ProviderName(); got != "composite[rest+mongo,mode=auto]" {
		t.Errorf("ProviderName(both) = %q", got)
	}
	restOnly := &CompositePrestacionLookupSource{rest: &AndesRESTPrestacionLookupSource{}, mode: "rest"}
	if got := restOnly.ProviderName(); got != "composite[rest,mode=rest]" {
		t.Errorf("ProviderName(rest only) = %q", got)
	}
}

func TestStudyUIDLikelyAndesIssued(t *testing.T) {
	prefixes := []string{"2.16.840.1.113883.2.10.35.1.200."}
	cases := []struct {
		name     string
		uid      string
		prefixes []string
		want     bool
	}{
		{"matches andes prefix", "2.16.840.1.113883.2.10.35.1.200.123", prefixes, true},
		{"foreign uid", "1.2.3.4.5", prefixes, false},
		{"empty uid", "", prefixes, false},
		{"no prefixes -> any non-empty uid", "1.2.3.4.5", nil, true},
		{"blank prefixes are skipped", "1.2.3.4.5", []string{"  ", ""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := studyUIDLikelyAndesIssued(tc.uid, tc.prefixes); got != tc.want {
				t.Errorf("studyUIDLikelyAndesIssued(%q)=%v want %v", tc.uid, got, tc.want)
			}
		})
	}
}

func TestHISConfigResolvers(t *testing.T) {
	providerCases := map[string]string{
		"mongo":   "mongo",
		"REST":    "rest", // case-insensitive
		"auto":    "auto",
		"":        "rest", // default
		"garbage": "rest", // unknown -> default
	}
	for in, want := range providerCases {
		c := HISConfig{PrestacionesProvider: in}
		if got := c.ResolvedPrestacionesProvider(); got != want {
			t.Errorf("ResolvedPrestacionesProvider(%q)=%q want %q", in, got, want)
		}
	}

	// Custom prefixes are trimmed and blanks dropped.
	custom := HISConfig{AndesUIDPrefixes: []string{" a ", "  ", "b"}}
	if got := custom.ResolvedAndesUIDPrefixes(); !equalStrings(got, []string{"a", "b"}) {
		t.Errorf("ResolvedAndesUIDPrefixes(custom) = %v want [a b]", got)
	}
	// Empty -> default prefixes.
	if got := (HISConfig{}).ResolvedAndesUIDPrefixes(); !equalStrings(got, defaultAndesUIDPrefixes) {
		t.Errorf("ResolvedAndesUIDPrefixes(empty) = %v want %v", got, defaultAndesUIDPrefixes)
	}
}
