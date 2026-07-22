package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
)

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func sampleFiles() []session.ChangedFile {
	return []session.ChangedFile{
		{Path: "/repo/a.go", Added: 10, Removed: 2},
		{Path: "/repo/pkg/b.go", Added: 0, Removed: 5},
		{Path: "/other/c.go", Added: 3, Removed: 0},
	}
}

func TestChangedFilesNavigationAndSelection(t *testing.T) {
	m := NewChangedFiles(40, 10)
	m.SetFiles(sampleFiles(), "/repo")

	if got := m.SelectedFile(); got != "/repo/a.go" {
		t.Fatalf("initial SelectedFile = %q, want /repo/a.go", got)
	}

	// j moves down; k moves back up; g/G jump to ends.
	m, _ = m.Update(key("j"))
	if got := m.SelectedFile(); got != "/repo/pkg/b.go" {
		t.Errorf("after j, SelectedFile = %q, want /repo/pkg/b.go", got)
	}

	m, _ = m.Update(key("G"))
	if got := m.SelectedFile(); got != "/other/c.go" {
		t.Errorf("after G, SelectedFile = %q, want /other/c.go", got)
	}

	// j at the bottom must not move past the end.
	m, _ = m.Update(key("j"))
	if got := m.SelectedFile(); got != "/other/c.go" {
		t.Errorf("after j at bottom, SelectedFile = %q, want /other/c.go", got)
	}

	m, _ = m.Update(key("g"))
	if got := m.SelectedFile(); got != "/repo/a.go" {
		t.Errorf("after g, SelectedFile = %q, want /repo/a.go", got)
	}
}

func TestChangedFilesSetFilesResetsCursor(t *testing.T) {
	m := NewChangedFiles(40, 10)
	m.SetFiles(sampleFiles(), "/repo")
	m, _ = m.Update(key("G"))

	// A new file set (new session) resets the cursor to the top.
	m.SetFiles([]session.ChangedFile{{Path: "/x/y.go"}}, "/x")
	if got := m.SelectedFile(); got != "/x/y.go" {
		t.Errorf("after SetFiles, SelectedFile = %q, want /x/y.go", got)
	}
}

func TestChangedFilesEmptyState(t *testing.T) {
	m := NewChangedFiles(40, 10)
	m.SetFiles(nil, "/repo")

	if got := m.SelectedFile(); got != "" {
		t.Errorf("SelectedFile with no files = %q, want empty", got)
	}
	if !strings.Contains(m.View(), "No files changed") {
		t.Error("empty view should show the 'No files changed' state")
	}
	// Navigation on an empty list must not panic or select anything.
	m, _ = m.Update(key("j"))
	if got := m.SelectedFile(); got != "" {
		t.Errorf("SelectedFile after j on empty list = %q, want empty", got)
	}
}

func TestChangedFilesViewShowsRelativePathAndCounts(t *testing.T) {
	m := NewChangedFiles(40, 10)
	m.SetFiles(sampleFiles(), "/repo")
	view := m.View()

	// Path is shown relative to the CWD, and the count column is rendered.
	if !strings.Contains(view, "pkg/b.go") {
		t.Errorf("view should contain the relative path pkg/b.go:\n%s", view)
	}
	if !strings.Contains(view, "+10") || !strings.Contains(view, "-2") {
		t.Errorf("view should contain the +10 -2 counts:\n%s", view)
	}
}
