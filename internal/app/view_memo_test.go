package app

import (
	"strings"
	"testing"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
	"github.com/thbits/naviClaude/internal/ui"
)

// TestViewMemoMatchesFreshRender walks state changes and checks each memoized
// frame equals one rendered with every cache invalidated.
func TestViewMemoMatchesFreshRender(t *testing.T) {
	t.Cleanup(func() { styles.ApplyTheme(styles.Named("")) })
	m, _ := clickModel(t)
	m.memo = &viewMemo{}
	m.cfg.Theme = "tokyo-night"

	resize := func(w, h int) func() {
		return func() { m.width, m.height = w, h; m.resizeComponents() }
	}
	steps := []struct {
		name string
		do   func()
	}{
		{"list", func() {}},
		{"capture", func() { m.preview.SetContent("first line\n\x1b[31mred\x1b[0m line\n") }},
		{"new capture", func() { m.preview.SetContent("second\n\x1b[1mbold\x1b[0m\n") }},
		{"long capture", func() { m.preview.SetContent(strings.Repeat("scrollback line\n", 80)) }},
		{"scroll preview", func() { m.preview.ScrollUp(5) }},
		{"breathing flip", func() {
			m.sessions[0].Status = session.StatusActive
			m.sidebar.SetSessions(m.sessions)
			for f := 1; f <= 3; f++ {
				m.sidebar.SetBreathingFrame(f)
			}
		}},
		{"cursor move", func() { m.sidebar.MoveCursor(1) }},
		{"hover", func() { m.sidebar.SetHover(1) }},
		{"passthrough", func() { m.mode = ModePassthrough }},
		{"back to list", func() { m.mode = ModeList }},
		{"right panel", func() {
			m.rightPanelOpen = true
			m.rightSidebar.SetFiles([]session.ChangedFile{{Path: "/w/a.go", Added: 2, Removed: 1}}, "/w")
		}},
		{"files focused", func() { m.mode = ModeChangedFiles }},
		{"right panel closed", func() { m.rightPanelOpen = false; m.mode = ModeList }},
		{"220x55", resize(220, 55)},
		{"120x40", resize(120, 40)},
		{"empty list", func() { m.sidebar.SetSessions(nil) }},
	}
	for _, st := range steps {
		st.do()
		m.View() // warm the memo with this state
		got := m.View()
		styles.ApplyTheme(styles.Named(m.cfg.Theme)) // same colors, but every memo misses
		if want := m.View(); got != want {
			t.Fatalf("%s: memoized frame differs from a fresh render\n got: %q\nwant: %q", st.name, got, want)
		}
	}
}

// TestThemeChangeInvalidatesMemo guards against a cached frame keeping old colors.
func TestThemeChangeInvalidatesMemo(t *testing.T) {
	t.Cleanup(func() { styles.ApplyTheme(styles.Named("")) })
	var memo ui.Memo[string]
	calls := 0
	render := func() string { calls++; return "x" }
	memo.Get("k", render)
	memo.Get("k", render)
	if calls != 1 {
		t.Fatalf("unchanged key rendered %d times, want 1", calls)
	}
	styles.ApplyTheme(styles.Named("dracula"))
	memo.Get("k", render)
	if calls != 2 {
		t.Errorf("theme change did not invalidate the memo (renders = %d)", calls)
	}
}
