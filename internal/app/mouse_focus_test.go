package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
)

// mouseModel builds a Model wide enough to have distinct pane regions, with one
// active session selected. width=100, sidebarWidthPct=0 -> sidebarWidth()=20
// (min clamp); with the right panel open rightSidebarWidth()=24, so the preview
// band is X in [20,76) and the right pane is X in [76,100).
func mouseModel(rightOpen bool) Model {
	sessions := []*session.Session{
		{ID: "s0", TmuxSession: "proj", TmuxTarget: "proj:0.0", CWD: "/home/u/proj", Status: session.StatusActive, LastActivity: time.Now()},
	}
	m := newFocusModel(0, sessions)
	m.width = 100
	m.height = 30
	m.rightPanelOpen = rightOpen
	m.sidebar.SelectByTarget("proj:0.0")
	return m
}

func TestMouseClickPreviewEntersPassthrough(t *testing.T) {
	m := mouseModel(false)
	m.mode = ModeList
	updated, _ := m.handleMouse(tea.MouseMsg{Type: tea.MouseLeft, X: 50, Y: 5})
	got := updated.(Model)
	if got.mode != ModePassthrough {
		t.Errorf("mode = %v, want passthrough", got.mode)
	}
	if got.focusedPane() != PanePreview {
		t.Errorf("focusedPane = %v, want PanePreview", got.focusedPane())
	}
}

func TestMouseClickSidebarFocusesList(t *testing.T) {
	m := mouseModel(false)
	m.mode = ModePassthrough
	m.preview.SetPassthrough(true)
	updated, _ := m.handleMouse(tea.MouseMsg{Type: tea.MouseLeft, X: 5, Y: 3})
	got := updated.(Model)
	if got.mode != ModeList {
		t.Errorf("mode = %v, want list", got.mode)
	}
	if got.focusedPane() != PaneList {
		t.Errorf("focusedPane = %v, want PaneList", got.focusedPane())
	}
}

func TestMouseClickRightPaneFocusesChangedFiles(t *testing.T) {
	m := mouseModel(true)
	m.mode = ModeList
	updated, _ := m.handleMouse(tea.MouseMsg{Type: tea.MouseLeft, X: 90, Y: 4})
	got := updated.(Model)
	if got.mode != ModeChangedFiles {
		t.Errorf("mode = %v, want changed-files", got.mode)
	}
	if got.focusedPane() != PaneFiles {
		t.Errorf("focusedPane = %v, want PaneFiles", got.focusedPane())
	}
}

// With the right panel closed, a click near the right edge is still the preview
// band (the changed-files region does not exist), so it enters passthrough.
func TestMouseClickRightEdgeWithPanelClosedIsPreview(t *testing.T) {
	m := mouseModel(false)
	m.mode = ModeList
	updated, _ := m.handleMouse(tea.MouseMsg{Type: tea.MouseLeft, X: 90, Y: 4})
	got := updated.(Model)
	if got.mode != ModePassthrough {
		t.Errorf("mode = %v, want passthrough", got.mode)
	}
}

// Clicking the preview with no session that can receive passthrough leaves
// focus unchanged.
func TestMouseClickPreviewNoSelectionNoop(t *testing.T) {
	m := newFocusModel(0, nil)
	m.width = 100
	m.height = 30
	m.mode = ModeList
	updated, _ := m.handleMouse(tea.MouseMsg{Type: tea.MouseLeft, X: 50, Y: 5})
	got := updated.(Model)
	if got.mode != ModeList {
		t.Errorf("mode = %v, want list (no-op)", got.mode)
	}
}

// A click while a modal is visible must not change pane focus underneath it.
func TestMouseIgnoredWhenModalActive(t *testing.T) {
	m := mouseModel(false)
	m.mode = ModeList
	m.help.Toggle()
	if !m.help.IsVisible() {
		t.Fatal("precondition: help overlay should be visible")
	}
	updated, _ := m.handleMouse(tea.MouseMsg{Type: tea.MouseLeft, X: 50, Y: 5})
	got := updated.(Model)
	if got.mode != ModeList {
		t.Errorf("mode = %v, want list unchanged (click ignored under modal)", got.mode)
	}
}
