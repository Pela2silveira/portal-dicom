package main

import (
	"reflect"
	"testing"
)

func TestBuildPhysicianLocalCacheFindQuery(t *testing.T) {
	t.Run("empty filters produce empty query", func(t *testing.T) {
		got := buildPhysicianLocalCacheFindQuery(PhysicianSearchFilters{}, "", "")
		if len(got) != 0 {
			t.Fatalf("expected empty query, got %v", got)
		}
	})

	t.Run("patient id is passed through verbatim", func(t *testing.T) {
		got := buildPhysicianLocalCacheFindQuery(PhysicianSearchFilters{PatientID: "  8025031 "}, "", "")
		if got["PatientID"] != "8025031" {
			t.Errorf("PatientID = %q want 8025031", got["PatientID"])
		}
		if _, ok := got["PatientName"]; ok {
			t.Errorf("did not expect PatientName key: %v", got)
		}
	})

	t.Run("patient name uses fuzzy wildcards", func(t *testing.T) {
		got := buildPhysicianLocalCacheFindQuery(PhysicianSearchFilters{PatientName: "perez"}, "", "")
		if got["PatientName"] != "*PEREZ*" {
			t.Errorf("PatientName = %q want *PEREZ*", got["PatientName"])
		}
	})

	t.Run("modality and study date range", func(t *testing.T) {
		got := buildPhysicianLocalCacheFindQuery(PhysicianSearchFilters{Modality: "CT"}, "2026-07-01", "2026-07-02")
		if got["ModalitiesInStudy"] != "CT" {
			t.Errorf("ModalitiesInStudy = %q want CT", got["ModalitiesInStudy"])
		}
		if got["StudyDate"] != "20260701-20260702" {
			t.Errorf("StudyDate = %q want 20260701-20260702", got["StudyDate"])
		}
	})

	t.Run("blank date only produces no StudyDate key", func(t *testing.T) {
		got := buildPhysicianLocalCacheFindQuery(PhysicianSearchFilters{PatientID: "1"}, "", "")
		if _, ok := got["StudyDate"]; ok {
			t.Errorf("did not expect StudyDate key when no dates given: %v", got)
		}
	})
}

func TestParseDICOMMultiValue(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{"empty", "", nil},
		{"blank", "   ", nil},
		{"single", "CT", []string{"CT"}},
		{"multi", `CT\SR`, []string{"CT", "SR"}},
		{"trims and drops blanks", ` CT \\ MR `, []string{"CT", "MR"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseDICOMMultiValue(tc.value); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseDICOMMultiValue(%q) = %v want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestParseNonNegativeInt(t *testing.T) {
	cases := map[string]int{
		"":      0,
		"  ":    0,
		"0":     0,
		" 154 ": 154,
		"-3":    0,
		"abc":   0,
		"12.5":  0,
	}
	for in, want := range cases {
		if got := parseNonNegativeInt(in); got != want {
			t.Errorf("parseNonNegativeInt(%q) = %d want %d", in, got, want)
		}
	}
}
