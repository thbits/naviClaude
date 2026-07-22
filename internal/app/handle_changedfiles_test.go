package app

import (
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

func TestResolveEditorFields(t *testing.T) {
	t.Run("configured editor wins over $EDITOR and splits flags", func(t *testing.T) {
		t.Setenv("EDITOR", "nvim")
		got := resolveEditorFields("cursor --wait")
		want := []string{"cursor", "--wait"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("fields = %v, want %v", got, want)
		}
	})

	t.Run("falls back to $EDITOR when unconfigured", func(t *testing.T) {
		t.Setenv("EDITOR", "nvim")
		if got := resolveEditorFields(""); len(got) != 1 || got[0] != "nvim" {
			t.Errorf("fields = %v, want [nvim]", got)
		}
	})

	t.Run("falls back to vi when nothing is set", func(t *testing.T) {
		t.Setenv("EDITOR", "")
		if got := resolveEditorFields(""); len(got) != 1 || got[0] != "vi" {
			t.Errorf("fields = %v, want [vi]", got)
		}
	})
}

func TestIsGUIEditor(t *testing.T) {
	gui := []string{"cursor", "code", "zed", "subl", "windsurf", "CURSOR", "/usr/local/bin/code", "code.exe"}
	for _, e := range gui {
		if !isGUIEditor(e) {
			t.Errorf("isGUIEditor(%q) = false, want true", e)
		}
	}
	terminal := []string{"vi", "vim", "nvim", "nano", "emacs", "hx", "micro", "/usr/bin/vim"}
	for _, e := range terminal {
		if isGUIEditor(e) {
			t.Errorf("isGUIEditor(%q) = true, want false", e)
		}
	}
}

func TestApplyGitCounts(t *testing.T) {
	// Use nonexistent paths so EvalSymlinks is a no-op and matching is exact.
	t.Run("live git diff replaces churn and clears estimate", func(t *testing.T) {
		files := []session.ChangedFile{
			{Path: "/repo/a.go", Added: 9, Removed: 9, Estimated: true}, // churn
		}
		stats := map[string]session.DiffStat{
			"/repo/a.go": {Added: 3, Removed: 1},
		}
		applyGitCounts(files, stats)

		if files[0].Added != 3 || files[0].Removed != 1 {
			t.Errorf("counts = +%d -%d, want +3 -1", files[0].Added, files[0].Removed)
		}
		if files[0].Estimated {
			t.Error("file with a live git diff should not be Estimated")
		}
	})

	t.Run("committed file keeps transcript estimate", func(t *testing.T) {
		files := []session.ChangedFile{
			{Path: "/repo/committed.go", Added: 12, Removed: 4, Estimated: true},
		}
		// stats present (in a repo) but this file has no pending diff.
		applyGitCounts(files, map[string]session.DiffStat{"/repo/other.go": {Added: 1}})

		if files[0].Added != 12 || files[0].Removed != 4 {
			t.Errorf("counts = +%d -%d, want +12 -4 (estimate kept)", files[0].Added, files[0].Removed)
		}
		if !files[0].Estimated {
			t.Error("committed file (no pending diff) should stay Estimated")
		}
	})

	t.Run("non-repo (nil stats) keeps estimates untouched", func(t *testing.T) {
		files := []session.ChangedFile{
			{Path: "/repo/a.go", Added: 5, Removed: 2, Estimated: true},
		}
		applyGitCounts(files, nil)

		if files[0].Added != 5 || files[0].Removed != 2 || !files[0].Estimated {
			t.Errorf("nil stats should leave the estimate unchanged, got +%d -%d est=%v",
				files[0].Added, files[0].Removed, files[0].Estimated)
		}
	})
}
