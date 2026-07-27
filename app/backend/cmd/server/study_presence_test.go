package main

import "testing"

func TestApplyStudyLocalPresence(t *testing.T) {
	const uid = "1.2.3"
	stone := buildStoneViewerURL(uid)
	ohif := buildOHIFViewerURL(uid)

	t.Run("not local keeps fallback state and no viewer urls", func(t *testing.T) {
		cache, status, phase, progress, viewer, ohifURL := applyStudyLocalPresence(uid, "not_local", "idle", "", 0, false, false, false)
		if cache != "not_local" || status != "idle" || phase != "" || progress != 0 {
			t.Fatalf("unexpected state: %s %s %s %d", cache, status, phase, progress)
		}
		if viewer != "" || ohifURL != "" {
			t.Fatalf("expected no viewer urls, got %q %q", viewer, ohifURL)
		}
	})

	t.Run("local idle is promoted to complete and viewable", func(t *testing.T) {
		cache, status, phase, progress, viewer, ohifURL := applyStudyLocalPresence(uid, "local_complete", "done", "done", 100, true, false, false)
		if cache != cacheStatusLocalComplete || status != "done" || phase != "done" || progress != 100 {
			t.Fatalf("unexpected state: %s %s %s %d", cache, status, phase, progress)
		}
		if viewer != stone || ohifURL != ohif {
			t.Fatalf("expected viewer urls, got %q %q", viewer, ohifURL)
		}
	})

	t.Run("local partial stays partial but viewable and terminal", func(t *testing.T) {
		cache, status, _, progress, viewer, _ := applyStudyLocalPresence(uid, cacheStatusLocalPartial, "idle", "", 0, true, false, false)
		if cache != cacheStatusLocalPartial {
			t.Fatalf("partial must not be masked as complete, got %s", cache)
		}
		if status != "done" || progress != 100 || viewer != stone {
			t.Fatalf("partial study should be viewable/terminal: %s %d %q", status, progress, viewer)
		}
	})

	t.Run("local unverified stays unverified", func(t *testing.T) {
		cache, _, _, _, viewer, _ := applyStudyLocalPresence(uid, cacheStatusLocalUnverified, "idle", "", 0, true, false, false)
		if cache != cacheStatusLocalUnverified {
			t.Fatalf("unverified must not be masked as complete, got %s", cache)
		}
		if viewer != stone {
			t.Fatalf("unverified study should still be viewable")
		}
	})

	t.Run("retrieve in flight is viewable but not forced to done", func(t *testing.T) {
		cache, status, phase, progress, viewer, _ := applyStudyLocalPresence(uid, "local_partial", "running", "downloading", 42, true, true, false)
		if status != "running" || phase != "downloading" || progress != 42 {
			t.Fatalf("in-flight retrieve state must be preserved: %s %s %d", status, phase, progress)
		}
		if cache != "local_partial" || viewer != stone {
			t.Fatalf("in-flight study should stay viewable with its cache flag: %s %q", cache, viewer)
		}
	})

	t.Run("failed retrieve is viewable but keeps retry state", func(t *testing.T) {
		_, status, _, _, viewer, _ := applyStudyLocalPresence(uid, "local_partial", "failed", "", 0, true, false, true)
		if status != "failed" {
			t.Fatalf("failed retrieve must not be masked as done, got %s", status)
		}
		if viewer != stone {
			t.Fatalf("failed-but-present study should still be viewable")
		}
	})
}
