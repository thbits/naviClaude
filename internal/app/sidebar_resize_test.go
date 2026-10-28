package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/config"
)

func drag(m *Model, x, y int) {
	next, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y})
	*m = next.(Model)
}

// resizeModel is clickModel with config saves captured instead of written.
func resizeModel(t *testing.T) (*Model, *[]config.Config) {
	m, _ := clickModel(t)
	var saved []config.Config
	m.saveConfig = func(c config.Config) error { saved = append(saved, c); return nil }
	m.cfg.SidebarWidth = 30
	return m, &saved
}

func TestDragSidebarBorderResizes(t *testing.T) {
	m, saved := resizeModel(t)
	border := m.sidebarWidth() - 1
	press(m, tea.MouseButtonLeft, border, yOne)
	drag(m, 44, yOne)
	if got := m.sidebarWidth(); got != 45 {
		t.Fatalf("width mid-drag = %d, want 45", got)
	}
	if len(*saved) != 0 {
		t.Fatalf("config saved mid-drag")
	}
	release(m, 49, yOne)
	if got := m.sidebarWidth(); got != 50 {
		t.Errorf("width after release = %d, want 50", got)
	}
	if len(*saved) != 1 || (*saved)[0].SidebarWidth != 50 {
		t.Errorf("saved = %+v, want one save with sidebar_width 50", *saved)
	}
	if m.draggingSidebar {
		t.Error("still dragging after release")
	}
	if got := selectedID(m); got != "a1" {
		t.Errorf("drag changed selection to %q", got)
	}
}

func TestDragSidebarClamps(t *testing.T) {
	tests := []struct {
		name      string
		rightOpen bool
		x         int
		want      int
	}{
		{"min width", false, 3, 20},
		{"preview keeps 20 columns", false, 95, 80},
		{"changed-files panel keeps room", true, 95, 56},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := resizeModel(t)
			m.rightPanelOpen = tt.rightOpen
			press(m, tea.MouseButtonLeft, m.sidebarWidth()-1, yOne)
			release(m, tt.x, yOne)
			if got := m.sidebarWidth(); got != tt.want {
				t.Errorf("width = %d, want %d", got, tt.want)
			}
			if tt.rightOpen && m.rightSidebarWidth() < 24 {
				t.Errorf("changed-files panel squeezed to %d", m.rightSidebarWidth())
			}
		})
	}
}

func TestMotionWithoutBorderPressDoesNotResize(t *testing.T) {
	m, saved := resizeModel(t)
	press(m, tea.MouseButtonLeft, xList, yOne)
	drag(m, 50, yOne)
	release(m, 50, yOne)
	if got := m.sidebarWidth(); got != 30 {
		t.Errorf("width = %d, want 30", got)
	}
	if len(*saved) != 0 {
		t.Errorf("config saved without a border drag")
	}
}

func TestUnchangedSidebarDragSkipsSave(t *testing.T) {
	m, saved := resizeModel(t)
	border := m.sidebarWidth() - 1
	press(m, tea.MouseButtonLeft, border, yOne)
	release(m, border, yOne)
	if len(*saved) != 0 {
		t.Errorf("saved %d times for a zero-length drag", len(*saved))
	}
}

func TestOpeningChangedFilesAfterWideDragKeepsPanel(t *testing.T) {
	m, _ := resizeModel(t)
	press(m, tea.MouseButtonLeft, m.sidebarWidth()-1, yOne)
	release(m, 79, yOne)
	if got := m.sidebarWidth(); got != 80 {
		t.Fatalf("precondition: width = %d, want 80", got)
	}
	m.rightPanelOpen = true
	if got := m.rightSidebarWidth(); got < 24 {
		t.Errorf("changed-files panel width = %d, want >= 24", got)
	}
	if got := m.width - m.sidebarWidth() - m.rightSidebarWidth(); got < 20 {
		t.Errorf("preview width = %d, want >= 20", got)
	}
	m.rightPanelOpen = false
	if got := m.sidebarWidth(); got != 80 {
		t.Errorf("width after closing the panel = %d, want 80", got)
	}
}

func TestTerminalResizeAfterDragUsesSavedPercent(t *testing.T) {
	m, saved := resizeModel(t)
	press(m, tea.MouseButtonLeft, m.sidebarWidth()-1, yOne)
	release(m, 39, yOne)
	if len(*saved) != 1 || (*saved)[0].SidebarWidth != 40 {
		t.Fatalf("precondition: saved = %+v, want sidebar_width 40", *saved)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	*m = next.(Model)
	if got := m.sidebarWidth(); got != 80 {
		t.Errorf("width at 200 columns = %d, want 80 (40%%)", got)
	}
}
