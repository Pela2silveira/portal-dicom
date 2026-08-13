package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOrthancRetrieveUsesConfiguredMode(t *testing.T) {
	cases := []struct {
		name           string
		mode           string
		wantOperation  string
		wantMethodName string
	}{
		{name: "c-get", mode: "c_get", wantOperation: "/modalities/hpn/get", wantMethodName: "C-GET"},
		{name: "c-move", mode: "c_move", wantOperation: "/modalities/hpn/move", wantMethodName: "C-MOVE"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			var gotPayload map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
					t.Errorf("decode request: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ID":"job-1"}`))
			}))
			defer server.Close()

			node := PACSNodeConfig{
				ID: "hpn",
				Retrieve: PACSNodeRetrieveConfig{
					Mode:      tc.mode,
					AET:       "PACSHPN",
					DICOMHost: "pacs.example",
					DICOMPort: 11112,
				},
			}
			app := &App{
				cfg:        Config{OrthancURL: server.URL},
				httpClient: server.Client(),
			}

			jobID, err := app.startOrthancRetrieve(context.Background(), node, "1.2.3")
			if err != nil {
				t.Fatalf("startOrthancRetrieve: %v", err)
			}
			if jobID != "job-1" {
				t.Fatalf("job id = %q, want job-1", jobID)
			}
			if gotPath != tc.wantOperation {
				t.Errorf("request path = %q, want %q", gotPath, tc.wantOperation)
			}
			if gotPayload["Level"] != "Study" {
				t.Errorf("request level = %#v, want Study", gotPayload["Level"])
			}
			if gotPayload["Asynchronous"] != true {
				t.Errorf("request asynchronous = %#v, want true", gotPayload["Asynchronous"])
			}

			rawModality, _, err := app.orthancModalityPayload(node)
			if err != nil {
				t.Fatalf("orthancModalityPayload: %v", err)
			}
			var modality orthancModalityRequest
			if err := json.Unmarshal(rawModality, &modality); err != nil {
				t.Fatalf("decode modality payload: %v", err)
			}
			if modality.RetrieveMethod != tc.wantMethodName {
				t.Errorf("retrieve method = %q, want %q", modality.RetrieveMethod, tc.wantMethodName)
			}
		})
	}
}

func TestOrthancRetrieveSeriesUsesConfiguredMode(t *testing.T) {
	var gotPath string
	var gotPayload struct {
		Level     string              `json:"Level"`
		Resources []map[string]string `json:"Resources"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ID":"job-series"}`))
	}))
	defer server.Close()

	node := PACSNodeConfig{
		ID: "hpn",
		Retrieve: PACSNodeRetrieveConfig{
			Mode:      "c_move",
			AET:       "PACSHPN",
			DICOMHost: "pacs.example",
			DICOMPort: 11112,
		},
	}
	app := &App{
		cfg:        Config{OrthancURL: server.URL},
		httpClient: server.Client(),
	}

	jobID, err := app.startOrthancRetrieveSeries(context.Background(), node, "1.2.3", []string{"4.5.6"})
	if err != nil {
		t.Fatalf("startOrthancRetrieveSeries: %v", err)
	}
	if jobID != "job-series" {
		t.Fatalf("job id = %q, want job-series", jobID)
	}
	if gotPath != "/modalities/hpn/move" {
		t.Errorf("request path = %q, want /modalities/hpn/move", gotPath)
	}
	if gotPayload.Level != "Series" {
		t.Errorf("request level = %q, want Series", gotPayload.Level)
	}
	if len(gotPayload.Resources) != 1 ||
		gotPayload.Resources[0]["StudyInstanceUID"] != "1.2.3" ||
		gotPayload.Resources[0]["SeriesInstanceUID"] != "4.5.6" {
		t.Errorf("unexpected resources: %#v", gotPayload.Resources)
	}
}

func TestRetrieveEffectiveProgress(t *testing.T) {
	cases := []struct {
		name   string
		status orthancRetrieveStatus
		want   int
	}{
		{
			name:   "orthanc progress wins when positive",
			status: orthancRetrieveStatus{Progress: 42, InstancesCount: 5, RemainingInstancesCount: 5},
			want:   42,
		},
		{
			name:   "derived from counters when orthanc progress is zero",
			status: orthancRetrieveStatus{Progress: 0, InstancesCount: 30, RemainingInstancesCount: 10},
			want:   75,
		},
		{
			name:   "all received derives to 100",
			status: orthancRetrieveStatus{Progress: 0, InstancesCount: 40, RemainingInstancesCount: 0},
			want:   100,
		},
		{
			name:   "no signal stays zero",
			status: orthancRetrieveStatus{Progress: 0, InstancesCount: 0, RemainingInstancesCount: 0},
			want:   0,
		},
		{
			name:   "clamped to 100 if counters overshoot",
			status: orthancRetrieveStatus{Progress: 0, InstancesCount: 200, RemainingInstancesCount: -50},
			want:   100,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retrieveEffectiveProgress(tc.status); got != tc.want {
				t.Fatalf("retrieveEffectiveProgress(%+v) = %d, want %d", tc.status, got, tc.want)
			}
		})
	}
}
