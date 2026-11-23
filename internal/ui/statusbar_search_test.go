package ui

import (
	"strings"
	"testing"
)

func TestStatusBar_PreviewSearchHints(t *testing.T) {
	sb := NewStatusBar(160, "1.0")
	sb.SetKeyHints(defaultListHints)
	sb.SetMode("preview search")
	line := hintLine(sb)
	for _, want := range []string{"⏎  keep matches", "Esc  clear", "↑/↓  prev/next"} {
		if !strings.Contains(line, want) {
			t.Errorf("query input: missing %q in %q", want, line)
		}
	}
	// The arrows name two keys, so a click on that hint replays nothing.
	if _, ok := sb.HintAt(strings.Index(line, "prev/next")); ok {
		t.Error("the ↑/↓ hint must be inert")
	}
}

// While matches stay highlighted n, N and Esc act on them, so the list's own
// hints for those keys must not be shown; help stays reachable.
func TestStatusBar_KeptPreviewSearchReplacesListHints(t *testing.T) {
	for _, hints := range [][]StatusHintInput{defaultListHints, nil} {
		sb := NewStatusBar(160, "1.0")
		if hints != nil {
			sb.SetKeyHints(hints)
		}
		sb.SetPreviewSearch(true)
		line := hintLine(sb)
		for _, want := range []string{"n  next match", "N  prev match", "Esc  clear search", "?  more"} {
			if !strings.Contains(line, want) {
				t.Errorf("missing %q in %q", want, line)
			}
		}
		for _, absent := range []string{"new", "kill", "open"} {
			if strings.Contains(line, absent) {
				t.Errorf("unexpected %q in %q", absent, line)
			}
		}
		// Other modes keep their own hints.
		sb.SetMode("passthrough")
		if line := hintLine(sb); strings.Contains(line, "next match") {
			t.Errorf("passthrough shows the match keys: %q", line)
		}
		sb.SetMode("list")
		sb.SetPreviewSearch(false)
		if line := hintLine(sb); strings.Contains(line, "next match") || !strings.Contains(line, "open") {
			t.Errorf("list hints did not come back: %q", line)
		}
	}
}
