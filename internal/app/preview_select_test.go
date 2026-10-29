package app

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/session"
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

func TestPreviewDragCopiesAndFocuses(t *testing.T) {
	m, copied := selectModel(t)
	press(m, tea.MouseButtonLeft, xPreview+2, yPreview)
	motion(m, xPreview+6, yPreview+1)
	cmd := releaseCmd(m, xPreview+3, yPreview+1)
	if m.mode != ModePassthrough {
		t.Errorf("mode = %v, want passthrough after a drag", m.mode)
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

// runPaste runs a right-click's command batch and returns the paste result.
func runPaste(t *testing.T, cmd tea.Cmd) clipboardPastedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("right-click should return a paste command")
	}
	msgs := []tea.Msg{cmd()}
	if batch, ok := msgs[0].(tea.BatchMsg); ok {
		msgs = msgs[:0]
		for _, c := range batch {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
	}
	for _, msg := range msgs {
		if p, ok := msg.(clipboardPastedMsg); ok {
			return p
		}
	}
	t.Fatal("no clipboardPastedMsg in the right-click's commands")
	return clipboardPastedMsg{}
}

func TestPreviewRightClickPastesAndFocuses(t *testing.T) {
	m, _ := selectModel(t)
	var target string
	m.pasteInto = func(tgt string) error { target = tgt; return nil }
	runPaste(t, press(m, tea.MouseButtonRight, xPreview+2, yPreview))
	if target != "zzmouse:0.1" {
		t.Errorf("pasted into %q, want the previewed pane zzmouse:0.1", target)
	}
	if m.mode != ModePassthrough {
		t.Errorf("mode = %v, want passthrough after a right-click paste", m.mode)
	}
}

func TestPreviewRightClickPasteError(t *testing.T) {
	m, _ := selectModel(t)
	m.pasteInto = func(string) error { return errors.New("clipboard has no text") }
	next, _ := m.Update(runPaste(t, press(m, tea.MouseButtonRight, xPreview+2, yPreview)))
	*m = next.(Model)
	if v := ansi.Strip(m.statusbar.View()); !strings.Contains(v, "paste failed") {
		t.Errorf("status bar = %q, want paste failed error", v)
	}
}

func TestPreviewRightClickIgnoresClosedSession(t *testing.T) {
	m, _ := selectModel(t)
	m.sidebar.SelectedSession().Status = session.StatusClosed
	called := false
	m.pasteInto = func(string) error { called = true; return nil }
	if cmd := press(m, tea.MouseButtonRight, xPreview+2, yPreview); cmd != nil {
		cmd()
	}
	if called || m.mode == ModePassthrough {
		t.Errorf("right-click on a closed session's preview pasted=%v mode=%v", called, m.mode)
	}
}
