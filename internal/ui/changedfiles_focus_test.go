package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

func changedFilesWithOne() ChangedFilesModel {
	m := NewChangedFiles(30, 12)
	m.SetFiles([]session.ChangedFile{
		{Path: "/repo/internal/app/app.go", Added: 3, Removed: 1},
	}, "/repo")
	m.SetSize(29, 11)
	return m
}

func TestChangedFilesTitleShowsFocusMarkerWhenFocused(t *testing.T) {
	m := changedFilesWithOne()
	m.SetFocused(true)
	if !strings.Contains(m.View(), "▸ CHANGED FILES") {
		t.Fatalf("focused changed-files title should contain the marker; got:\n%s", m.View())
	}
}

func TestChangedFilesTitleHasNoMarkerWhenUnfocused(t *testing.T) {
	m := changedFilesWithOne()
	m.SetFocused(false)
	v := m.View()
	if strings.Contains(v, "▸ CHANGED FILES") {
		t.Fatalf("unfocused changed-files title must not show the marker; got:\n%s", v)
	}
	if !strings.Contains(v, "CHANGED FILES") {
		t.Fatalf("unfocused changed-files should still show its title; got:\n%s", v)
	}
}

// TestChangedFilesStatDimsWhenUnfocused locks the dim invariant for the
// changed-files panel: the "+N" stat renders in ColorGreen when the pane is
// focused, and dims to ColorDimText when it is not. Would fail if the stat
// stayed green (or never dimmed) while the pane lost focus.
func TestChangedFilesStatDimsWhenUnfocused(t *testing.T) {
	forceColorProfile(t)

	m := changedFilesWithOne()
	greenPlus := lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("+3")
	dimPlus := lipgloss.NewStyle().Foreground(styles.ColorDimText).Render("+3")

	m.SetFocused(true)
	v := m.View()
	if !strings.Contains(v, greenPlus) {
		t.Fatalf("focused changed-files must render the +N stat in green; got:\n%s", v)
	}
	if strings.Contains(v, dimPlus) {
		t.Fatalf("focused changed-files must not dim the +N stat; got:\n%s", v)
	}

	m.SetFocused(false)
	v = m.View()
	if strings.Contains(v, greenPlus) {
		t.Fatalf("unfocused changed-files must not render the +N stat in green; got:\n%s", v)
	}
	if !strings.Contains(v, dimPlus) {
		t.Fatalf("unfocused changed-files must dim the +N stat; got:\n%s", v)
	}
}
