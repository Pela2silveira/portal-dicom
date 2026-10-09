package main

import (
	"encoding/json"
	"testing"
)

func TestDicomwebHostKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"https://pacssss.andes.gob.ar/dcm4chee-arc/aets/PACSHRDLS/rs", "https://pacssss.andes.gob.ar"},
		{"https://PACSSSS.andes.gob.ar/dcm4chee-arc/aets/PACSHTM/rs/", "https://pacssss.andes.gob.ar"},
		{"https://pacshpn.andes.gob.ar/dcm4chee-arc/aets/PACSHPN/rs", "https://pacshpn.andes.gob.ar"},
		{"", ""},
		{"not-a-url", ""},
	}
	for _, tc := range cases {
		if got := dicomwebHostKey(tc.in); got != tc.want {
			t.Errorf("dicomwebHostKey(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestClassifyStudyByRetrieveAET(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		enabled      bool
		expectedAET  string
		retrieveAETs []string
		wantKeep     bool
		wantReason   string
	}{
		{
			name:         "disabled keeps mismatched",
			enabled:      false,
			expectedAET:  "PACSHRDLS",
			retrieveAETs: []string{"PACSHMM"},
			wantKeep:     true,
			wantReason:   "disabled",
		},
		{
			name:         "matched keep",
			enabled:      true,
			expectedAET:  "PACSHRDLS",
			retrieveAETs: []string{"PACSHRDLS"},
			wantKeep:     true,
			wantReason:   "matched",
		},
		{
			name:         "matched case insensitive",
			enabled:      true,
			expectedAET:  "pacshrdls",
			retrieveAETs: []string{"PACSHRDLS"},
			wantKeep:     true,
			wantReason:   "matched",
		},
		{
			name:         "matched among multi-value",
			enabled:      true,
			expectedAET:  "PACSHRDLS",
			retrieveAETs: []string{"OTHER", "PACSHRDLS"},
			wantKeep:     true,
			wantReason:   "matched",
		},
		{
			name:         "mismatched drop",
			enabled:      true,
			expectedAET:  "PACSHRDLS",
			retrieveAETs: []string{"PACSHMM"},
			wantKeep:     false,
			wantReason:   "mismatched_retrieve_aet",
		},
		{
			name:         "empty retrieve aet drop",
			enabled:      true,
			expectedAET:  "PACSHRDLS",
			retrieveAETs: nil,
			wantKeep:     false,
			wantReason:   "empty_retrieve_aet",
		},
		{
			name:         "no expected aet keep",
			enabled:      true,
			expectedAET:  "",
			retrieveAETs: []string{"PACSHMM"},
			wantKeep:     true,
			wantReason:   "no_expected_aet",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyStudyByRetrieveAET(tc.enabled, tc.expectedAET, tc.retrieveAETs)
			if got.Keep != tc.wantKeep || got.Reason != tc.wantReason {
				t.Fatalf("got keep=%v reason=%q want keep=%v reason=%q", got.Keep, got.Reason, tc.wantKeep, tc.wantReason)
			}
		})
	}
}

func TestRetrieveAETitlesFromItem(t *testing.T) {
	t.Parallel()

	item := qidoResponseItem{
		"00080054": dicomJSONAttribute{
			Value: []json.RawMessage{mustMarshalJSON("PACSHMM"), mustMarshalJSON("PACSHTM")},
		},
	}
	got := retrieveAETitlesFromItem(item)
	if len(got) != 2 || got[0] != "PACSHMM" || got[1] != "PACSHTM" {
		t.Fatalf("retrieveAETitlesFromItem = %#v", got)
	}
}

func TestShouldClassifyByRetrieveAETAutoSharedHost(t *testing.T) {
	t.Parallel()

	hrdls := PACSNodeConfig{
		ID:   "hrdls",
		Name: "Hospital Rincón de los Sauces",
		Search: PACSNodeSearchConfig{
			Mode:            "qido_rs",
			DICOMwebBaseURL: "https://pacssss.andes.gob.ar/dcm4chee-arc/aets/PACSHRDLS/rs",
		},
		Retrieve: PACSNodeRetrieveConfig{AET: "PACSHRDLS"},
	}
	htm := PACSNodeConfig{
		ID:   "htm",
		Name: "Hospital Tricao Malal",
		Search: PACSNodeSearchConfig{
			Mode:            "qido_rs",
			DICOMwebBaseURL: "https://pacssss.andes.gob.ar/dcm4chee-arc/aets/PACSHTM/rs",
		},
		Retrieve: PACSNodeRetrieveConfig{AET: "PACSHTM"},
	}
	hpn := PACSNodeConfig{
		ID:   "hpn",
		Name: "Hospital Provincial Neuquén",
		Search: PACSNodeSearchConfig{
			Mode:            "qido_rs",
			DICOMwebBaseURL: "https://pacshpn.andes.gob.ar/dcm4chee-arc/aets/PACSHPN/rs",
		},
		Retrieve: PACSNodeRetrieveConfig{AET: "PACSHPN"},
	}

	app := &App{externalConfig: &ExternalConfig{PACSNodes: []PACSNodeConfig{hrdls, htm, hpn}}}

	if !app.shouldClassifyByRetrieveAET(hrdls) {
		t.Fatal("shared-host node must auto-enable classify_by_retrieve_aet")
	}
	if !app.shouldClassifyByRetrieveAET(htm) {
		t.Fatal("shared-host peer must auto-enable classify_by_retrieve_aet")
	}
	if app.shouldClassifyByRetrieveAET(hpn) {
		t.Fatal("dedicated-host node must stay off by default")
	}

	forcedOff := hrdls
	forcedOff.Search.ClassifyByRetrieveAET = boolPtr(false)
	if app.shouldClassifyByRetrieveAET(forcedOff) {
		t.Fatal("explicit false must override auto-on")
	}

	forcedOn := hpn
	forcedOn.Search.ClassifyByRetrieveAET = boolPtr(true)
	if !app.shouldClassifyByRetrieveAET(forcedOn) {
		t.Fatal("explicit true must force classify on")
	}
}

func TestNodeExpectedRetrieveAET(t *testing.T) {
	t.Parallel()

	node := PACSNodeConfig{
		ID:  "hrdls",
		AET: "LEGACY",
		Retrieve: PACSNodeRetrieveConfig{
			AET: "PACSHRDLS",
		},
	}
	if got := nodeExpectedRetrieveAET(node); got != "PACSHRDLS" {
		t.Fatalf("nodeExpectedRetrieveAET = %q want PACSHRDLS", got)
	}
}
