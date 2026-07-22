package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

// forceColorProfile forces lipgloss to emit real ANSI escape codes for the
// duration of a test. Outside a TTY (i.e. under `go test`), lipgloss's default
// renderer detects NoColor and Render() returns plain, unstyled text -- so
// tests that need to assert on actual escape sequences must force a profile,
// and restore the original one afterward so other tests are unaffected.
func forceColorProfile(t *testing.T) {
	t.Helper()
	orig := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(orig) })
}

func focusSidebarWithOneSession() SidebarModel {
	m := NewSidebar(30, 12)
	m.SetSessions([]*session.Session{{
		ID:           "abcd1234",
		TmuxSession:  "work",
		TmuxTarget:   "work:1.0",
		ProjectName:  "myproj",
		Status:       session.StatusActive,
		LastActivity: time.Now(),
	}})
	m.SetSize(29, 11)
	return m
}

func TestSidebarTitleShowsFocusMarkerWhenFocused(t *testing.T) {
	m := focusSidebarWithOneSession()
	m.SetFocused(true)
	if !strings.Contains(m.View(), "▸ SESSIONS") {
		t.Fatalf("focused sidebar title should contain the marker; got:\n%s", m.View())
	}
}

func TestSidebarTitleHasNoFocusMarkerWhenUnfocused(t *testing.T) {
	m := focusSidebarWithOneSession()
	m.SetFocused(false)
	v := m.View()
	if strings.Contains(v, "▸ SESSIONS") {
		t.Fatalf("unfocused sidebar title must not show the focus marker; got:\n%s", v)
	}
	if !strings.Contains(v, "SESSIONS") {
		t.Fatalf("unfocused sidebar should still show the SESSIONS title; got:\n%s", v)
	}
}

func TestSidebarWidthUnchangedByFocus(t *testing.T) {
	m := focusSidebarWithOneSession()
	m.SetFocused(true)
	focusedW := lipgloss.Width(m.View())
	m.SetFocused(false)
	unfocusedW := lipgloss.Width(m.View())
	if focusedW != unfocusedW {
		t.Fatalf("focus must not change rendered width: focused=%d unfocused=%d", focusedW, unfocusedW)
	}
}

// TestSidebarUnfocusedKeepsStatusDotColoredButDimsText locks the core dim
// invariant: dimming an inactive pane must never hide session status. The
// status dot for a StatusActive session keeps its semantic ColorGreen even
// when the pane is unfocused, while the surrounding name text dims to
// ColorDimText. Would fail if the dot were dimmed along with the text.
func TestSidebarUnfocusedKeepsStatusDotColoredButDimsText(t *testing.T) {
	forceColorProfile(t)

	m := focusSidebarWithOneSession()
	// Move the cursor onto the group header so the single session renders via
	// the normal (non-cursor) row path -- the cursor/selected row intentionally
	// stays bright regardless of focus (see renderSessionItem), so it would not
	// exercise the dimming logic under test here.
	m.SetCursor(0)
	// Pin the breathing animation to a frame that resolves StatusActive to
	// ColorGreen: statusIconProps toggles on frame%4<2, so frame 0 is green.
	m.SetBreathingFrame(0)
	m.SetFocused(false)
	v := m.View()

	greenDot := lipgloss.NewStyle().Foreground(styles.ColorGreen).Render(styles.IconActive)
	if !strings.Contains(v, greenDot) {
		t.Fatalf("unfocused sidebar must keep the active status dot colored green; got:\n%s", v)
	}

	dimName := lipgloss.NewStyle().Foreground(styles.ColorDimText).Render("myproj")
	if !strings.Contains(v, dimName) {
		t.Fatalf("unfocused sidebar must dim the session name text; got:\n%s", v)
	}
}

// TestSidebarNonSelectedNameDimsOnlyWhenUnfocused asserts the name text of a
// non-cursor row is ColorDimText when the pane is unfocused, and is NOT
// ColorDimText (uses the normal SidebarProjectName color) when focused.
func TestSidebarNonSelectedNameDimsOnlyWhenUnfocused(t *testing.T) {
	forceColorProfile(t)

	m := focusSidebarWithOneSession()
	m.SetCursor(0) // cursor on the group header; the session renders unselected
	dimName := lipgloss.NewStyle().Foreground(styles.ColorDimText).Render("myproj")

	m.SetFocused(true)
	if v := m.View(); strings.Contains(v, dimName) {
		t.Fatalf("focused sidebar must not dim the session name; got:\n%s", v)
	}

	m.SetFocused(false)
	if v := m.View(); !strings.Contains(v, dimName) {
		t.Fatalf("unfocused sidebar must dim the session name; got:\n%s", v)
	}
}
