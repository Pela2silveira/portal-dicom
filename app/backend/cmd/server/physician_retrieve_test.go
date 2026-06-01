package main

import (
	"context"
	"testing"
)

func physicianRetrieveTestApp() *App {
	return &App{
		externalConfig: &ExternalConfig{
			PACSNodes: []PACSNodeConfig{
				{ID: "hpn"},
				{ID: "hhh"},
			},
		},
	}
}

func TestPreferredConfiguredNodeID(t *testing.T) {
	app := physicianRetrieveTestApp()

	cases := map[string]string{
		"hpn":     "hpn",
		"  hhh  ": "hhh",
		"ghost":   "",
		"":        "",
		"   ":     "",
		"HPN":     "", // node ids are matched exactly (case-sensitive)
	}
	for input, want := range cases {
		if got := app.preferredConfiguredNodeID(input); got != want {
			t.Fatalf("preferredConfiguredNodeID(%q): want %q, got %q", input, want, got)
		}
	}
}

func TestResolvePhysicianRetrieveSourceNodePreferredValid(t *testing.T) {
	app := physicianRetrieveTestApp()

	// A valid preferred node id is authoritative and must short-circuit before
	// any DB-backed inference (app.db is nil here on purpose).
	got, err := app.resolvePhysicianRetrieveSourceNode(context.Background(), "physician-1", "1.2.3", "  hpn  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hpn" {
		t.Fatalf("want resolved node \"hpn\", got %q", got)
	}
}
