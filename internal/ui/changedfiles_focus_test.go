package ui

import (
	"strings"
	"testing"

	"github.com/thbits/naviClaude/internal/session"
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
