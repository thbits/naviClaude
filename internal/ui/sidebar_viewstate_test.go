package ui

import (
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

func twoGroupSidebar() SidebarModel {
	m := NewSidebar(30, 12)
	m.SetSessions([]*session.Session{
		{ID: "a1", TmuxSession: "hermes", TmuxTarget: "hermes:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now()},
		{ID: "b1", TmuxSession: "default", TmuxTarget: "default:1.0", ProjectName: "p2", Status: session.StatusActive, LastActivity: time.Now()},
	})
	m.SetSize(29, 11)
	return m
}

func TestSeedToggledGroupsCollapsesGroup(t *testing.T) {
	m := NewSidebar(30, 12)
	m.SeedToggledGroups(map[string]bool{"hermes": true})
	m.SetSessions([]*session.Session{
		{ID: "a1", TmuxSession: "hermes", TmuxTarget: "hermes:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now()},
	})
	m.SetSize(29, 11)
	// A collapsed group hides its session rows: only the group header is a flat item.
	for _, it := range m.FlatItems() {
		if !it.IsGroup && it.Session != nil && it.Session.ID == "a1" {
			t.Fatal("seeded-collapsed group should hide its session row")
		}
	}
}

func TestSeedToggledGroupsSurvivesAutoCollapse(t *testing.T) {
	m := NewSidebar(30, 12)
	m.SetCollapseAfterHours(1) // aggressive auto-collapse
	m.SeedToggledGroups(map[string]bool{"hermes": false})
	m.SetSessions([]*session.Session{
		// Stale activity would normally auto-collapse the group.
		{ID: "a1", TmuxSession: "hermes", TmuxTarget: "hermes:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now().Add(-48 * time.Hour)},
	})
	m.SetSize(29, 11)
	// Seeded as expanded (and thus user-toggled), so auto-collapse must not hide it.
	found := false
	for _, it := range m.FlatItems() {
		if !it.IsGroup && it.Session != nil && it.Session.ID == "a1" {
			found = true
		}
	}
	if !found {
		t.Fatal("seeded-expanded group must survive auto-collapse")
	}
}

func TestToggledGroupsReflectsManualToggle(t *testing.T) {
	m := twoGroupSidebar()
	// ExpandGroup marks a group user-toggled; first collapse it via seed so the
	// expand is a real state change.
	m.SeedToggledGroups(map[string]bool{"hermes": true})
	m.SetSessions([]*session.Session{
		{ID: "a1", TmuxSession: "hermes", TmuxTarget: "hermes:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now()},
	})
	m.ExpandGroup("hermes")
	got := m.ToggledGroups()
	if v, ok := got["hermes"]; !ok || v != false {
		t.Errorf("ToggledGroups()[hermes] = (%v,%v), want (false,true)", v, ok)
	}
}
