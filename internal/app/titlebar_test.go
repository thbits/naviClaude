package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/session"
)

func summaryModel(width int) Model {
	now := time.Now()
	m := newFocusModel(0, []*session.Session{
		{ID: "w1", Status: session.StatusActive, LastActivity: now},
		{ID: "w2", Status: session.StatusActive, LastActivity: now},
		{ID: "q1", Status: session.StatusWaiting, LastActivity: now},
		{ID: "d1", Status: session.StatusIdle, Attention: "done", LastActivity: now},
		{ID: "i1", Status: session.StatusIdle, LastActivity: now},
		{ID: "i2", Status: session.StatusIdle, LastActivity: now},
		{ID: "i3", Status: session.StatusIdle, LastActivity: now},
		{ID: "c1", Status: session.StatusClosed, LastActivity: now},
	})
	m.width, m.height = width, 30
	m.currentTmuxSession = "work"
	return m
}

func TestCountStatuses(t *testing.T) {
	got := countStatuses(summaryModel(200).sessions)
	want := statusCounts{working: 2, waiting: 1, done: 1, idle: 3}
	if got != want {
		t.Errorf("countStatuses = %+v, want %+v", got, want)
	}
}

func TestTitleBarShowsStatusSummaryOnce(t *testing.T) {
	m := summaryModel(200)
	bar := m.renderTitleBar()
	if h := lipgloss.Height(bar); h != 1 {
		t.Fatalf("title bar is %d rows, want 1", h)
	}
	plain := ansi.Strip(bar)
	for _, want := range []string{"naviClaude", "● 2 working", "◎ 1 needs you", "◉ 1 done", "○ 3 idle", "work"} {
		if !strings.Contains(plain, want) {
			t.Errorf("title bar missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "sessions") || strings.Contains(plain, "closed") {
		t.Errorf("title bar should not repeat a total or count closed sessions: %q", plain)
	}
}

func TestTitleBarSkipsEmptyStatuses(t *testing.T) {
	m := newFocusModel(0, []*session.Session{{ID: "w1", Status: session.StatusActive}})
	m.width = 120
	plain := ansi.Strip(m.renderTitleBar())
	if !strings.Contains(plain, "● 1 working") {
		t.Errorf("missing working count: %q", plain)
	}
	for _, absent := range []string{"needs you", "idle", "done"} {
		if strings.Contains(plain, absent) {
			t.Errorf("zero-count %q should be omitted: %q", absent, plain)
		}
	}
}

// Narrow titles fall back to bare counts, then drop the summary, and always
// stay one row no wider than the terminal.
func TestTitleBarSummaryDegradesWhenNarrow(t *testing.T) {
	compact := ansi.Strip(summaryModel(50).renderTitleBar())
	if !strings.Contains(compact, "● 2 · ◎ 1 · ◉ 1 · ○ 3") || strings.Contains(compact, "working") {
		t.Errorf("width 50: want counts without labels, got %q", compact)
	}
	for _, width := range []int{200, 80, 50, 30, 20} {
		bar := summaryModel(width).renderTitleBar()
		if h, w := lipgloss.Height(bar), lipgloss.Width(bar); h != 1 || w > width {
			t.Errorf("width %d: title bar is %dx%d", width, w, h)
		}
	}
	if tiny := ansi.Strip(summaryModel(20).renderTitleBar()); strings.Contains(tiny, "●") {
		t.Errorf("width 20: summary should be dropped, got %q", tiny)
	}
}

// List hints follow configured keys and keep help last.
func TestStatusHintsFollowConfig(t *testing.T) {
	km := KeyMapFromConfig(config.KeyBindings{Jump: "g", Help: "h"})
	hints := km.StatusHints()
	if len(hints) > 6 {
		t.Fatalf("got %d list hints, want at most 6", len(hints))
	}
	if hints[1].Key != "g" || hints[len(hints)-1].Key != "h" || hints[len(hints)-1].Desc != "more" {
		t.Errorf("hints = %+v, want configured jump key and help last", hints)
	}
}

// Every list action dropped from the status bar is still in the help overlay.
func TestHelpListsEveryAction(t *testing.T) {
	km := DefaultKeyMap()
	help := map[string]bool{}
	for _, b := range km.HelpBindings() {
		help[b.Key] = true
	}
	for _, key := range []string{km.Search, km.Jump, km.NewSession, km.NewTmuxSession, km.RenameSession,
		km.KillSession, km.ToggleChanged, km.Detail, km.Stats, km.Help, km.Quit, KeyThemePicker, "Ctrl+U/Ctrl+D", "Tab/Shift+Tab"} {
		if !help[key] {
			t.Errorf("help overlay is missing key %q", key)
		}
	}
}

func TestChangedFilesHintClick(t *testing.T) {
	m, _ := clickModel(t)
	m.rightPanelOpen = true
	m.resizeComponents()
	m.setFocus(ModeChangedFiles)

	// Enter opens the (empty) selection in the editor; it must not act as the
	// list's Enter and drop into passthrough.
	press(m, tea.MouseButtonLeft, hintX(t, m, "Enter"), m.height-1)
	if m.mode != ModeChangedFiles {
		t.Fatalf("mode = %v after clicking open, want changed files", m.mode)
	}
	press(m, tea.MouseButtonLeft, hintX(t, m, "Esc"), m.height-1)
	if m.mode != ModeList {
		t.Errorf("mode = %v after clicking back, want list", m.mode)
	}
}
