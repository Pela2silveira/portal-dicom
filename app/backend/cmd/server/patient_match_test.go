package main

import "testing"

// Characterization tests for the patient-authorization matching kernel.
// These rules are security-critical: a false positive in patientDemographicMatch
// would let a logged-in patient see another patient's remote study. We lock in
// the current behavior (birth-date AND sex AND a "high" fuzzy name match).

func TestHasHighPatientNameMatch(t *testing.T) {
	cases := []struct {
		name        string
		left, right string
		want        bool
	}{
		{"exact normalized", "Pérez Juan", "PEREZ JUAN", true},
		{"reordered tokens", "PEREZ JUAN", "JUAN PEREZ", true},
		{"dicom caret separator", "PEREZ JUAN", "PEREZ^JUAN", true},
		{"single shared surname is not enough", "PEREZ JUAN", "PEREZ", false},
		{"three tokens, one differs", "PEREZ JUAN CARLOS", "PEREZ JUAN LUIS", true},
		{"disjoint names", "PEREZ JUAN", "GOMEZ LUIS", false},
		{"empty left", "", "PEREZ JUAN", false},
		{"empty right", "PEREZ JUAN", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasHighPatientNameMatch(tc.left, tc.right); got != tc.want {
				t.Errorf("hasHighPatientNameMatch(%q,%q)=%v want %v", tc.left, tc.right, got, tc.want)
			}
		})
	}
}

func TestPatientDemographicMatch(t *testing.T) {
	patient := PatientSummary{
		FullName:  "PEREZ JUAN",
		BirthDate: "1980-05-10",
		Sex:       "M",
	}
	cases := []struct {
		name      string
		candidate remotePatientMatchCandidate
		want      bool
	}{
		{
			name:      "full match (ISO birthdate, M sex)",
			candidate: remotePatientMatchCandidate{PatientName: "PEREZ^JUAN", BirthDate: "1980-05-10", Sex: "M"},
			want:      true,
		},
		{
			name:      "full match with DICOM birthdate and sex synonym",
			candidate: remotePatientMatchCandidate{PatientName: "PEREZ^JUAN", BirthDate: "19800510", Sex: "Masculino"},
			want:      true,
		},
		{
			name:      "birthdate mismatch blocks",
			candidate: remotePatientMatchCandidate{PatientName: "PEREZ^JUAN", BirthDate: "1981-05-10", Sex: "M"},
			want:      false,
		},
		{
			name:      "sex mismatch blocks",
			candidate: remotePatientMatchCandidate{PatientName: "PEREZ^JUAN", BirthDate: "1980-05-10", Sex: "F"},
			want:      false,
		},
		{
			name:      "name mismatch blocks even if birthdate+sex match",
			candidate: remotePatientMatchCandidate{PatientName: "GOMEZ^LUIS", BirthDate: "1980-05-10", Sex: "M"},
			want:      false,
		},
		{
			name:      "empty remote birthdate blocks",
			candidate: remotePatientMatchCandidate{PatientName: "PEREZ^JUAN", BirthDate: "", Sex: "M"},
			want:      false,
		},
		{
			name:      "empty remote sex blocks",
			candidate: remotePatientMatchCandidate{PatientName: "PEREZ^JUAN", BirthDate: "1980-05-10", Sex: ""},
			want:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := patientDemographicMatch(patient, tc.candidate); got != tc.want {
				t.Errorf("patientDemographicMatch = %v want %v", got, tc.want)
			}
		})
	}

	t.Run("empty HIS birthdate blocks", func(t *testing.T) {
		p := PatientSummary{FullName: "PEREZ JUAN", BirthDate: "", Sex: "M"}
		c := remotePatientMatchCandidate{PatientName: "PEREZ^JUAN", BirthDate: "1980-05-10", Sex: "M"}
		if patientDemographicMatch(p, c) {
			t.Errorf("empty HIS birthdate should block")
		}
	})
}
