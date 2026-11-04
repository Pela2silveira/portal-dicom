package main

import (
	"strings"
	"testing"
)

func TestPhysicianSearchOriginsUsesPreferredName(t *testing.T) {
	nodes := []PACSNodeConfig{
		{ID: "hpnsyn", Name: "Hospital Provincial Neuquén (Synapse)", Priority: 2, SearchOrigin: "Provincial-Neuquen"},
		{ID: "hc", Name: "Hospital Centenario", Priority: 1},
		{ID: "hpn", Name: "Hospital Provincial Neuquén", Priority: 1, SearchOrigin: "provincial-neuquen"},
	}

	origins := physicianSearchOrigins(nodes)
	if len(origins) != 1 {
		t.Fatalf("origins = %d, want 1", len(origins))
	}
	if origins[0].ID != "provincial-neuquen" {
		t.Fatalf("origin id = %q", origins[0].ID)
	}
	if origins[0].Label != "Hospital Provincial Neuquén" {
		t.Fatalf("label = %q", origins[0].Label)
	}
	if len(origins[0].Nodes) != 2 || origins[0].Nodes[0].ID != "hpn" || origins[0].Nodes[1].ID != "hpnsyn" {
		t.Fatalf("members = %#v", origins[0].Nodes)
	}

	selected := pacsNodesForSearchSource(nodes, "provincial-neuquen")
	if len(selected) != 2 || selected[0].ID != "hpn" || selected[1].ID != "hpnsyn" {
		t.Fatalf("selected = %#v", selected)
	}
	if got := pacsNodesForSearchSource(nodes, "hpn"); got != nil {
		t.Fatalf("node id must not expand, got %#v", got)
	}
	if got := pacsNodesForSearchSource(nodes, "local_cache"); got != nil {
		t.Fatalf("local cache must not expand, got %#v", got)
	}
}

func TestMergePhysicianResultsByStudyKeepsPreferredNode(t *testing.T) {
	merged := mergePhysicianResultsByStudy([][]PhysicianResult{
		{
			{StudyInstanceUID: "1.2.shared", StudyDate: "20261001", SourceNodeID: "hpn"},
			{StudyInstanceUID: "1.2.hpn-only", StudyDate: "20260901", SourceNodeID: "hpn"},
		},
		{
			{StudyInstanceUID: "1.2.shared", StudyDate: "20261001", SourceNodeID: "hpnsyn"},
			{StudyInstanceUID: "1.2.syn-only", StudyDate: "20261101", SourceNodeID: "hpnsyn"},
		},
	})
	if len(merged) != 3 {
		t.Fatalf("len = %d, want 3", len(merged))
	}
	if merged[0].StudyInstanceUID != "1.2.syn-only" || merged[0].SourceNodeID != "hpnsyn" {
		t.Fatalf("first = %#v", merged[0])
	}
	if merged[1].StudyInstanceUID != "1.2.shared" || merged[1].SourceNodeID != "hpn" {
		t.Fatalf("shared result = %#v", merged[1])
	}
	if merged[2].StudyInstanceUID != "1.2.hpn-only" {
		t.Fatalf("last = %#v", merged[2])
	}
}

func TestValidateSearchOrigins(t *testing.T) {
	err := validateSearchOrigins([]PACSNodeConfig{
		{ID: "hpn", SearchOrigin: "hpn"},
	})
	if err == nil || !strings.Contains(err.Error(), "must not match a node id") {
		t.Fatalf("node id clash error = %v", err)
	}

	err = validateSearchOrigins([]PACSNodeConfig{
		{ID: "hpn", SearchOrigin: "local_cache"},
	})
	if err == nil || !strings.Contains(err.Error(), "local_cache") {
		t.Fatalf("local_cache error = %v", err)
	}

	if err := validateSearchOrigins([]PACSNodeConfig{
		{ID: "hpn", SearchOrigin: "provincial-neuquen"},
		{ID: "hpnsyn", SearchOrigin: "provincial-neuquen"},
		{ID: "hc"},
	}); err != nil {
		t.Fatal(err)
	}
}
