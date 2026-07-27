package main

import "testing"

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
