package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
)

// Wheel down/up moves the changed-files selection like j/k so wheel-over-pane
// scrolling works once app.handleMouse routes wheel events here.
func TestChangedFilesWheelMovesCursor(t *testing.T) {
	m := NewChangedFiles(30, 12)
	m.SetFiles([]session.ChangedFile{
		{Path: "/repo/a.go"},
		{Path: "/repo/b.go"},
		{Path: "/repo/c.go"},
	}, "/repo")

	if got := m.SelectedFile(); got != "/repo/a.go" {
		t.Fatalf("initial selected = %q, want /repo/a.go", got)
	}

	m, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	if got := m.SelectedFile(); got != "/repo/b.go" {
		t.Fatalf("after wheel down selected = %q, want /repo/b.go", got)
	}

	m, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelUp})
	if got := m.SelectedFile(); got != "/repo/a.go" {
		t.Fatalf("after wheel up selected = %q, want /repo/a.go", got)
	}
}

// Wheel up at the top and wheel down at the bottom must clamp, not wrap.
func TestChangedFilesWheelClamps(t *testing.T) {
	m := NewChangedFiles(30, 12)
	m.SetFiles([]session.ChangedFile{{Path: "/repo/a.go"}, {Path: "/repo/b.go"}}, "/repo")

	m, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelUp}) // already at top
	if got := m.SelectedFile(); got != "/repo/a.go" {
		t.Fatalf("wheel up at top selected = %q, want /repo/a.go", got)
	}

	m, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	m, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown}) // past bottom
	if got := m.SelectedFile(); got != "/repo/b.go" {
		t.Fatalf("wheel down past bottom selected = %q, want /repo/b.go", got)
	}
}
