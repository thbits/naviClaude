package ui

import (
	"strings"
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

func previewWithSession() PreviewModel {
	m := NewPreview(60, 12)
	m.SetSession(&session.Session{
		ProjectName: "myproj",
		TmuxTarget:  "work:1.0",
		Status:      session.StatusActive,
	})
	return m
}

func TestPreviewShowsFocusChipWhenPassthrough(t *testing.T) {
	m := previewWithSession()
	m.SetPassthrough(true)
	if !strings.Contains(m.View(), "▸ myproj") {
		t.Fatalf("focused preview header should contain the lit chip; got:\n%s", m.View())
	}
}

func TestPreviewHasNoFocusChipWhenNotPassthrough(t *testing.T) {
	m := previewWithSession()
	m.SetPassthrough(false)
	v := m.View()
	if strings.Contains(v, "▸ myproj") {
		t.Fatalf("unfocused preview header must not show the focus chip; got:\n%s", v)
	}
	if !strings.Contains(v, "myproj") {
		t.Fatalf("preview header should still show the project name; got:\n%s", v)
	}
}
