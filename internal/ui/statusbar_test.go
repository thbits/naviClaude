package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestStatusBar_UpdateLabel(t *testing.T) {
	sb := NewStatusBar(120, "1.2.3")

	if strings.Contains(sb.View(), "update available") {
		t.Fatal("label should be absent before SetUpdateAvailable(true)")
	}

	sb.SetUpdateAvailable(true)
	if !strings.Contains(sb.View(), "update available") {
		t.Error("label should be present after SetUpdateAvailable(true)")
	}
	if !strings.Contains(sb.View(), "1.2.3") {
		t.Error("version should still be present alongside the label")
	}
}

// The rendered bar must never grow taller than its normal height: overflow
// wraps to an extra line, and app.go's contentHeight math then shifts the
// whole layout up a row. On narrow widths the update label is dropped (and,
// as a last resort, the bar truncated) rather than wrapped.
func TestStatusBar_NoOverflowOnNarrowWidth(t *testing.T) {
	wide := NewStatusBar(200, "1.2.3")
	wantHeight := lipgloss.Height(wide.View())

	for _, width := range []int{80, 60, 40, 20} {
		sb := NewStatusBar(width, "1.2.3")
		sb.SetUpdateAvailable(true)
		view := sb.View()

		if h := lipgloss.Height(view); h != wantHeight {
			t.Errorf("width %d: status bar height = %d, want %d (bar wrapped)", width, h, wantHeight)
		}
		for _, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line width = %d, want <= %d", width, w, width)
			}
		}
	}

	// The error path shares the bar and must not wrap either.
	sb := NewStatusBar(20, "1.2.3")
	sb.SetError("some very long error message that cannot possibly fit")
	if h := lipgloss.Height(sb.View()); h != wantHeight {
		t.Errorf("error path: status bar height = %d, want %d (bar wrapped)", h, wantHeight)
	}
}

// defaultListHints mirrors app.KeyMap.StatusHints for the default bindings.
var defaultListHints = []StatusHintInput{
	{Key: "Enter", Desc: "open"},
	{Key: "f", Desc: "jump"},
	{Key: "n", Desc: "new"},
	{Key: "/", Desc: "search"},
	{Key: "F", Desc: "files"},
	{Key: "K", Desc: "kill"},
	{Key: "?", Desc: "more"},
}

// hintLine is the bar's text row (below its top border), without escapes.
func hintLine(sb StatusBarModel) string {
	lines := strings.Split(sb.View(), "\n")
	return ansi.Strip(lines[len(lines)-1])
}

func TestStatusBar_HintsPerMode(t *testing.T) {
	tests := []struct {
		mode   string
		want   []string
		absent []string
	}{
		{"list", []string{"⏎  open", "f  jump", "n  new", "/  search", "K  kill", "?  more"}, []string{"rename", "stats", "detail"}},
		{"passthrough", []string{"Shift+Tab  back", "Ctrl+]  list", "Ctrl+F  jump"}, []string{"more", "back to list"}},
		{"help", []string{"any key  close"}, []string{"kill", "more"}},
		{"changed files", []string{"⏎/e  open in editor", "Esc  back"}, []string{"kill", "more"}},
		{"search", []string{"Esc  cancel", "⏎  select"}, []string{"kill"}},
		{"contextmenu", []string{"j/k  navigate", "⏎  select", "Esc  cancel"}, []string{"kill"}},
		{"rename", []string{"⏎  confirm", "Esc  cancel"}, []string{"kill"}},
		{"dir picker", []string{"Esc  cancel"}, []string{"kill"}},
	}
	for _, tt := range tests {
		sb := NewStatusBar(160, "1.0")
		sb.SetKeyHints(defaultListHints)
		sb.SetMode(tt.mode)
		line := hintLine(sb)
		// Keycaps carry one cell of padding, so "⏎  open" is "[ ⏎ ] open".
		for _, w := range tt.want {
			if !strings.Contains(line, w) {
				t.Errorf("mode %q: missing %q in %q", tt.mode, w, line)
			}
		}
		for _, a := range tt.absent {
			if strings.Contains(line, a) {
				t.Errorf("mode %q: unexpected %q in %q", tt.mode, a, line)
			}
		}
	}

	sb := NewStatusBar(160, "1.0")
	sb.SetKeyHints(defaultListHints)
	if n := len(sb.hintsForMode()); n > 7 {
		t.Errorf("list mode shows %d hints, want at most 7", n)
	}
	if line := hintLine(sb); strings.Index(line, "more") < strings.Index(line, "kill") {
		t.Errorf("help hint must come last; got %q", line)
	}
}

// Narrowing the bar drops whole hints from the low-priority end while the
// pinned help hint stays, and never cuts a description.
func TestStatusBar_NarrowDropsLowestPriority(t *testing.T) {
	descs := []string{"open", "jump", "new", "search", "files", "kill"}
	prev := len(descs) + 1
	for width := 120; width >= 12; width-- {
		sb := NewStatusBar(width, "1.0")
		sb.SetKeyHints(defaultListHints)
		line := hintLine(sb)
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("width %d: bar is %d wide", width, w)
		}
		if !strings.Contains(line, "?  more") {
			t.Errorf("width %d: pinned help hint missing: %q", width, line)
		}
		shown := 0
		for i, d := range descs {
			if strings.Contains(line, " "+d) {
				if shown != i {
					t.Errorf("width %d: %q shown after a higher-priority hint was dropped: %q", width, d, line)
				}
				shown++
			}
		}
		if shown+1 > prev {
			t.Errorf("width %d: hint count grew while narrowing (%d > %d)", width, shown+1, prev)
		}
		prev = shown + 1
		if spans := len(sb.spansForTest()); spans != shown+1 {
			t.Errorf("width %d: %d clickable spans for %d rendered hints", width, spans, shown+1)
		}
	}
	if prev != 1 {
		t.Errorf("at the narrowest width only the help hint should remain, got %d hints", prev)
	}
}

// Every column HintAt reports must sit on that hint's own rendered text, in
// every mode and at narrow widths.
func TestStatusBar_HintAtMatchesRenderedText(t *testing.T) {
	for _, mode := range []string{"list", "passthrough", "changed files", "search", "contextmenu", "themepicker", "stats", "help"} {
		for _, width := range []int{160, 80, 50, 30} {
			sb := NewStatusBar(width, "1.0")
			sb.SetKeyHints(defaultListHints)
			sb.SetMode(mode)
			line := hintLine(sb)
			for _, h := range sb.fittedHints() {
				_, spans := sb.layoutHints()
				var span hintSpan
				for _, s := range spans {
					if s.key == h.key {
						span = s
					}
				}
				want := " " + h.keycap() + "  " + h.desc
				if got := ansi.Cut(line, span.start, span.end); got != want {
					t.Errorf("mode %q width %d: span %q covers %q, want %q", mode, width, h.key, got, want)
				}
				for x := span.start; x < span.end; x++ {
					if !sb.OnHint(x) {
						t.Errorf("mode %q width %d: OnHint(%d) = false inside %q", mode, width, x, h.key)
						break
					}
					k, ok := sb.HintAt(x)
					if h.inert {
						if ok {
							t.Errorf("mode %q width %d: inert %q replays %q", mode, width, h.key, k)
						}
						break
					}
					if !ok || k != h.key {
						t.Errorf("mode %q width %d: HintAt(%d) = %q,%v want %q", mode, width, x, k, ok, h.key)
						break
					}
				}
			}
		}
	}
}

// Hints naming no single key (j/k) sit on the bar but replay nothing, and the
// gaps between hints are not on any hint.
func TestStatusBar_InertHintsReplayNothing(t *testing.T) {
	for _, mode := range []string{"contextmenu", "themepicker"} {
		sb := NewStatusBar(160, "1.0")
		sb.SetMode(mode)
		_, spans := sb.layoutHints()
		if len(spans) < 2 || spans[0].key != "j/k" {
			t.Fatalf("mode %q: spans = %+v, want j/k first", mode, spans)
		}
		x := spans[0].start
		if !sb.OnHint(x) {
			t.Errorf("mode %q: OnHint on j/k = false", mode)
		}
		if k, ok := sb.HintAt(x); ok {
			t.Errorf("mode %q: HintAt on j/k = %q, want no replayable key", mode, k)
		}
		if k, ok := sb.HintAt(spans[1].start); !ok || k != "Enter" {
			t.Errorf("mode %q: HintAt on Enter = %q,%v", mode, k, ok)
		}
		if gap := spans[0].end; sb.OnHint(gap) {
			t.Errorf("mode %q: separator column %d reported as a hint", mode, gap)
		}
	}
}

// The kill prompt replaces whatever the mode's hints are until it is cleared.
func TestStatusBar_ConfirmKillHints(t *testing.T) {
	sb := NewStatusBar(160, "1.0")
	sb.SetKeyHints(defaultListHints)
	sb.SetConfirmKill(true)
	line := hintLine(sb)
	for _, w := range []string{"y  kill", "any key  cancel"} {
		if !strings.Contains(line, w) {
			t.Errorf("confirm kill: missing %q in %q", w, line)
		}
	}
	if strings.Contains(line, "more") || strings.Contains(line, "search") {
		t.Errorf("confirm kill must hide the list hints: %q", line)
	}
	sb.SetConfirmKill(false)
	if line := hintLine(sb); !strings.Contains(line, "?  more") {
		t.Errorf("after confirm kill: list hints not restored: %q", line)
	}
}

func (m StatusBarModel) spansForTest() []hintSpan {
	_, spans := m.layoutHints()
	return spans
}
