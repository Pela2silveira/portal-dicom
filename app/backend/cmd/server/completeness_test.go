package main

import (
	"reflect"
	"testing"
)

func TestEvaluateStudyCompleteness(t *testing.T) {
	t.Run("empty expected is not evaluated", func(t *testing.T) {
		got := evaluateStudyCompleteness(seriesCounts{}, seriesCounts{"1.1": 10})
		if got.Evaluated {
			t.Fatalf("expected Evaluated=false when no source counts, got %+v", got)
		}
		if got.Complete {
			t.Fatalf("expected Complete=false when not evaluated, got %+v", got)
		}
	})

	t.Run("all series present with enough instances is complete", func(t *testing.T) {
		expected := seriesCounts{"1.1": 100, "1.2": 50}
		local := seriesCounts{"1.1": 100, "1.2": 50}
		got := evaluateStudyCompleteness(expected, local)
		if !got.Evaluated || !got.Complete {
			t.Fatalf("want evaluated+complete, got %+v", got)
		}
		if len(got.MissingSeries) != 0 {
			t.Fatalf("want no missing series, got %v", got.MissingSeries)
		}
		if got.ExpectedInstances != 150 || got.PresentInstances != 150 {
			t.Fatalf("instance totals wrong: %+v", got)
		}
	})

	t.Run("missing series is incomplete", func(t *testing.T) {
		expected := seriesCounts{"1.1": 100, "1.2": 50}
		local := seriesCounts{"1.1": 100}
		got := evaluateStudyCompleteness(expected, local)
		if got.Complete {
			t.Fatalf("want incomplete, got %+v", got)
		}
		if !reflect.DeepEqual(got.MissingSeries, []string{"1.2"}) {
			t.Fatalf("want missing [1.2], got %v", got.MissingSeries)
		}
		if got.PresentSeries != 1 || got.ExpectedSeries != 2 {
			t.Fatalf("series counts wrong: %+v", got)
		}
	})

	t.Run("short series (fewer instances) is incomplete", func(t *testing.T) {
		expected := seriesCounts{"1.1": 100}
		local := seriesCounts{"1.1": 90}
		got := evaluateStudyCompleteness(expected, local)
		if got.Complete {
			t.Fatalf("want incomplete for short series, got %+v", got)
		}
		if !reflect.DeepEqual(got.MissingSeries, []string{"1.1"}) {
			t.Fatalf("want missing [1.1], got %v", got.MissingSeries)
		}
	})

	t.Run("unknown expected instance count only checks presence", func(t *testing.T) {
		expected := seriesCounts{"1.1": 0, "1.2": 0}
		local := seriesCounts{"1.1": 5, "1.2": 0}
		got := evaluateStudyCompleteness(expected, local)
		if !got.Complete {
			t.Fatalf("want complete when instance counts unknown and series present, got %+v", got)
		}
	})

	t.Run("missing series list is sorted", func(t *testing.T) {
		expected := seriesCounts{"1.3": 1, "1.1": 1, "1.2": 1}
		local := seriesCounts{}
		got := evaluateStudyCompleteness(expected, local)
		if !reflect.DeepEqual(got.MissingSeries, []string{"1.1", "1.2", "1.3"}) {
			t.Fatalf("want sorted missing series, got %v", got.MissingSeries)
		}
	})
}

func TestStudyCompletenessCompletionPercent(t *testing.T) {
	cases := []struct {
		name   string
		report studyCompletenessReport
		want   int
	}{
		{"prefers instances", studyCompletenessReport{ExpectedInstances: 200, PresentInstances: 100, ExpectedSeries: 2, PresentSeries: 2}, 50},
		{"falls back to series when no instance counts", studyCompletenessReport{ExpectedSeries: 4, PresentSeries: 1}, 25},
		{"complete is 100", studyCompletenessReport{ExpectedInstances: 10, PresentInstances: 10}, 100},
		{"clamps above 100", studyCompletenessReport{ExpectedInstances: 10, PresentInstances: 25}, 100},
		{"no expected is zero", studyCompletenessReport{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.report.completionPercent(); got != tc.want {
				t.Errorf("completionPercent() = %d want %d", got, tc.want)
			}
		})
	}
}
