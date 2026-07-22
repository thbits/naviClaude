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

func TestChangedFilesSetFilesPreservesCursorOnRefresh(t *testing.T) {
	m := NewChangedFiles(40, 10)
	m.SetFiles(sampleFiles(), "/repo")
	m, _ = m.Update(key("j")) // move to /repo/pkg/b.go

	// A live refresh returns the same session's files, possibly with a new one
	// prepended (indices shift). The cursor should stay on the same file.
	refreshed := append([]session.ChangedFile{{Path: "/repo/new.go", Added: 1}}, sampleFiles()...)
	m.SetFiles(refreshed, "/repo")

	if got := m.SelectedFile(); got != "/repo/pkg/b.go" {
		t.Errorf("after refresh, SelectedFile = %q, want /repo/pkg/b.go (cursor should follow the file)", got)
	}
}

func TestChangedFilesLongNameKeepsCounts(t *testing.T) {
	m := NewChangedFiles(20, 6) // narrow panel
	m.SetFiles([]session.ChangedFile{
		{Path: "/repo/internal/app/handle_changedfiles.go", Added: 123, Removed: 45},
	}, "/repo")

	view := m.View()
	// Counts must never be cut off, even when the name does not fit.
	if !strings.Contains(view, "+123") || !strings.Contains(view, "-45") {
		t.Errorf("counts were cut off in a narrow panel:\n%s", view)
	}
	// The name is truncated from the left, so a leading ellipsis appears.
	if !strings.Contains(view, "…") {
		t.Errorf("expected a leading ellipsis on the truncated name:\n%s", view)
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
