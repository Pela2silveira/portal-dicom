package main

import (
	"reflect"
	"testing"
)

func TestNormalizePatientIDSource(t *testing.T) {
	cases := map[string]string{
		"":                patientIDSourceDNI,
		"  ":              patientIDSourceDNI,
		"dni":             patientIDSourceDNI,
		"DNI":             patientIDSourceDNI,
		"document_number": patientIDSourceDNI,
		"documento":       patientIDSourceDNI,
		"mongo_id":        patientIDSourceMongoID,
		"MongoId":         patientIDSourceMongoID,
		"mongo_object_id": patientIDSourceMongoID,
		"his_field":       "his_field",
	}
	for input, want := range cases {
		if got := normalizePatientIDSource(input); got != want {
			t.Errorf("normalizePatientIDSource(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPatientIDSourceNeedsMongo(t *testing.T) {
	if !patientIDSourceNeedsMongo("mongo_id") {
		t.Error("mongo_id should need mongo")
	}
	if patientIDSourceNeedsMongo("dni") {
		t.Error("dni should not need mongo")
	}
	if patientIDSourceNeedsMongo("") {
		t.Error("default (dni) should not need mongo")
	}
}

func TestEffectivePatientIDForNode(t *testing.T) {
	ids := patientSearchIdentifiers{DocumentNumber: "20399527", MongoID: "5978aa6038b54a194fedbf65"}

	tests := []struct {
		name   string
		source string
		ids    patientSearchIdentifiers
		want   string
	}{
		{"default empty -> dni", "", ids, "20399527"},
		{"dni", "dni", ids, "20399527"},
		{"mongo_id", "mongo_id", ids, "5978aa6038b54a194fedbf65"},
		{"mongo alias", "mongo_object_id", ids, "5978aa6038b54a194fedbf65"},
		{"mongo missing -> empty", "mongo_id", patientSearchIdentifiers{DocumentNumber: "20399527"}, ""},
		{"unknown future field -> empty", "his_field", ids, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectivePatientIDForNode(tt.source, tt.ids); got != tt.want {
				t.Errorf("effectivePatientIDForNode(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

func TestCandidateLocalCachePatientIDs(t *testing.T) {
	ids := patientSearchIdentifiers{DocumentNumber: "20399527", MongoID: "5978aa6038b54a194fedbf65"}

	tests := []struct {
		name    string
		ids     patientSearchIdentifiers
		sources []string
		want    []string
	}{
		{
			name:    "only dni source",
			ids:     ids,
			sources: []string{"dni"},
			want:    []string{"20399527"},
		},
		{
			name:    "dni and mongo sources -> both, sorted, unique",
			ids:     ids,
			sources: []string{"dni", "mongo_id", "dni"},
			want:    []string{"20399527", "5978aa6038b54a194fedbf65"},
		},
		{
			name:    "empty sources defaults to dni",
			ids:     ids,
			sources: nil,
			want:    []string{"20399527"},
		},
		{
			name:    "mongo source but mongo unknown -> empty",
			ids:     patientSearchIdentifiers{DocumentNumber: "20399527"},
			sources: []string{"mongo_id"},
			want:    []string{},
		},
		{
			name:    "unknown provider source dropped",
			ids:     ids,
			sources: []string{"dni", "his_field"},
			want:    []string{"20399527"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := candidateLocalCachePatientIDs(tt.ids, tt.sources)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("candidateLocalCachePatientIDs = %#v, want %#v", got, tt.want)
			}
		})
	}
}
