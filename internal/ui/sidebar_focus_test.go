package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/session"
)

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
