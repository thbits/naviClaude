package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// clickAt sends a left press and release on the same cell.
func clickAt(m Model, x, y int) Model {
	next, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	next, _ = next.(Model).handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease, X: x, Y: y})
	return next.(Model)
}

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
	m.currentTmuxWin = "proj:0" // keeps selectPreviewSession from resizing a real tmux window
	m.sidebar.SelectByTarget("proj:0.0")
	return m
}

func TestMouseClickPreviewEntersPassthrough(t *testing.T) {
	m := mouseModel(false)
	m.mode = ModeList
	got := clickAt(m, 50, 5)
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
	// Y=20 is blank list space below the only group, so no row is hit.
	updated, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 5, Y: 20})
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
	updated, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 90, Y: 4})
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
	got := clickAt(m, 90, 4)
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
	updated, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 50, Y: 5})
	got := updated.(Model)
	if got.mode != ModeList {
		t.Errorf("mode = %v, want list (no-op)", got.mode)
	}
}

// A click inside a modal is a no-op; outside it closes the modal without
// acting on the pane underneath.
func TestMouseModalBlocksPanesUnderneath(t *testing.T) {
	m := mouseModel(false)
	m.mode = ModeList
	m.help.SetSize(m.width, m.height)
	m.help.Toggle()
	m.mode = ModeHelp
	x, y, w, h := ui.OverlayRect(m.width, m.height, m.help.View())

	updated, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x + w/2, Y: y + h/2})
	got := updated.(Model)
	if !got.help.IsVisible() || got.mode != ModeHelp {
		t.Fatalf("click inside help: visible = %v mode = %v, want it left open", got.help.IsVisible(), got.mode)
	}

	// Outside the popup, over the preview: closes help but must not enter passthrough.
	updated, _ = got.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: m.width - 1, Y: 3})
	got = updated.(Model)
	if got.help.IsVisible() || got.mode != ModeList {
		t.Errorf("click outside help: visible = %v mode = %v, want closed in list mode", got.help.IsVisible(), got.mode)
	}
}
