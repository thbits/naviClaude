package ui

import (
	"testing"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

func TestRepoSegmentsAheadBehind(t *testing.T) {
	files := []session.ChangedFile{{Path: "a", Added: 3, Removed: 1}}
	for _, tc := range []struct {
		ahead, behind int
		want          string
	}{
		{2, 1, "⎇ main ↑2↓1 · 1 file +3/−1"},
		{2, 0, "⎇ main ↑2 · 1 file +3/−1"},
		{0, 4, "⎇ main ↓4 · 1 file +3/−1"},
		{0, 0, "⎇ main · 1 file +3/−1"},
	} {
		ref := session.GitRef{Repo: "naviClaude", Branch: "main", Ahead: tc.ahead, Behind: tc.behind}
		if got := plainSegments(repoSegments(ref, files, 80)); got != styles.IconRepo+" naviClaude "+tc.want {
			t.Errorf("↑%d↓%d: %q", tc.ahead, tc.behind, got)
		}
	}
}
