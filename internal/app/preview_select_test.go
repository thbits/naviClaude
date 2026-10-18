package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Preview content origin in clickModel: right of the 30-column sidebar plus the
// preview's 1-column padding, below the title bar and 2-row preview header.
const (
	xPreview = 31
	yPreview = titleRows + 2
)

func motion(m *Model, x, y int) {
	next, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y})
	*m = next.(Model)
}

func releaseCmd(m *Model, x, y int) tea.Cmd {
	next, cmd := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease, X: x, Y: y})
	*m = next.(Model)
	return cmd
}

func selectModel(t *testing.T) (*Model, *string) {
	t.Helper()
	m, _ := clickModel(t)
	m.preview.SetContent("alpha line   \nbeta line\ngamma")
	var copied string
	m.copyText = func(s string) error { copied = s; return nil }
	return m, &copied
}

func TestPreviewDragCopiesWithoutFocus(t *testing.T) {
	m, copied := selectModel(t)
	press(m, tea.MouseButtonLeft, xPreview+2, yPreview)
	motion(m, xPreview+6, yPreview+1)
	cmd := releaseCmd(m, xPreview+3, yPreview+1)
	if m.mode != ModeList {
		t.Errorf("mode = %v, want list: a drag must not enter passthrough", m.mode)
	}
	if cmd == nil {
		t.Fatal("drag should return a copy command")
	}
	next, _ := m.Update(cmd())
	*m = next.(Model)
	if want := "pha line\nbeta"; *copied != want {
		t.Errorf("copied %q, want %q", *copied, want)
	}
	if v := m.statusbar.View(); !strings.Contains(ansi.Strip(v), "copied 2 lines") {
		t.Errorf("status bar = %q, want copied notice", v)
	}
}

func TestPreviewClickFocusesPassthrough(t *testing.T) {
	m, copied := selectModel(t)
	press(m, tea.MouseButtonLeft, xPreview+2, yPreview)
	if m.mode != ModeList {
		t.Fatalf("press alone changed mode to %v", m.mode)
	}
	releaseCmd(m, xPreview+2, yPreview)
	if m.mode != ModePassthrough {
		t.Errorf("mode = %v, want passthrough after a click", m.mode)
	}
	if *copied != "" {
		t.Errorf("click copied %q", *copied)
	}
}

func TestPreviewDragIntoSidebarClamps(t *testing.T) {
	m, copied := selectModel(t)
	press(m, tea.MouseButtonLeft, xPreview+4, yPreview+1)
	motion(m, xList, yPreview)
	cmd := releaseCmd(m, xList, yPreview)
	if selectedID(m) != "a1" {
		t.Errorf("drag into the sidebar selected %q", selectedID(m))
	}
	if cmd == nil {
		t.Fatal("drag should return a copy command")
	}
	cmd()
	if want := "alpha line\nbeta"; *copied != want {
		t.Errorf("copied %q, want %q", *copied, want)
	}
}

func TestPreviewHoldsCapturesDuringDrag(t *testing.T) {
	m, _ := selectModel(t)
	press(m, tea.MouseButtonLeft, xPreview, yPreview)
	motion(m, xPreview+3, yPreview)
	m.preview.SetContent("replaced")
	if v := m.preview.View(); !strings.Contains(ansi.Strip(v), "alpha") || strings.Contains(ansi.Strip(v), "replaced") {
		t.Fatalf("capture applied mid-drag:\n%s", v)
	}
	releaseCmd(m, xPreview+3, yPreview)
	if v := m.preview.View(); !strings.Contains(ansi.Strip(v), "replaced") {
		t.Errorf("held capture not applied after release:\n%s", v)
	}
}

func TestPreviewKeyEndsStuckDrag(t *testing.T) {
	m, _ := selectModel(t)
	press(m, tea.MouseButtonLeft, xPreview, yPreview)
	motion(m, xPreview+3, yPreview)
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	*m = next.(Model)
	if m.preview.Selecting() {
		t.Error("a key press should end a drag whose release was lost")
	}
}
