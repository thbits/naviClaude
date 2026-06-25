package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
)

// firstSessionID returns the ID of the first non-group session row.
func firstSessionID(m *SidebarModel) string {
	for _, it := range m.FlatItems() {
		if !it.IsGroup && it.Session != nil {
			return it.Session.ID
		}
	}
	return ""
}

func TestGroupRecentActivity(t *testing.T) {
	now := time.Now()
	sessions := []*session.Session{
		{ID: "a", LastActivity: now.Add(-2 * time.Hour)},
		{ID: "b", LastActivity: now}, // newest, not first in slice
		{ID: "c", LastActivity: now.Add(-time.Hour)},
	}
	got := groupRecentActivity(sessions)
	if !got.Equal(now) {
		t.Errorf("groupRecentActivity = %v, want %v (newest)", got, now)
	}
	if groupRecentActivity(nil) != (time.Time{}) {
		t.Error("groupRecentActivity(nil) should be the zero time")
	}
}

// TestGroupActivitySortUsesNewest verifies the activity group order is a total
// order keyed on each group's most-recent session, not on Sessions[0] (which,
// under name-sort-within-group, can be a stale session).
func TestGroupActivitySortUsesNewest(t *testing.T) {
	now := time.Now()
	m := NewSidebar(40, 20)
	m.SetGroupSortOrder("activity")
	m.SetSessionSortOrder("name") // so Sessions[0] is the alphabetically-first

	sessions := []*session.Session{
		// Group "older": its alphabetically-first session ("aaa") is OLD, but
		// the group also holds a fairly recent one.
		{ID: "o1", TmuxSession: "older", DisplayName: "aaa", LastActivity: now.Add(-90 * time.Minute)},
		{ID: "o2", TmuxSession: "older", DisplayName: "zzz", LastActivity: now.Add(-30 * time.Minute)},
		// Group "newer": its newest session is more recent than older's newest.
		{ID: "n1", TmuxSession: "newer", DisplayName: "mmm", LastActivity: now.Add(-5 * time.Minute)},
	}
	m.SetSessions(sessions)

	// "newer" group is most-recently-active, so its session row comes first.
	if got := firstSessionID(&m); got != "n1" {
		t.Errorf("first session = %q, want n1 (newer group should sort first)", got)
	}
}

// TestRestoreCursorSnapsToSession verifies that after a list change where the
// tracked selection vanishes, the cursor lands on a session row rather than a
// group header.
func TestRestoreCursorSnapsToSession(t *testing.T) {
	now := time.Now()
	m := NewSidebar(40, 20)

	// Initial load: no tracked selection. Cursor should snap onto the first
	// session row, not sit on the leading group header.
	sessions := []*session.Session{
		{ID: "s1", TmuxSession: "grp", DisplayName: "one", LastActivity: now},
		{ID: "s2", TmuxSession: "grp", DisplayName: "two", LastActivity: now},
	}
	m.SetSessions(sessions)

	if sel := m.SelectedSession(); sel == nil {
		t.Fatalf("after initial load cursor is on a header/empty, want a session row (cursor=%d)", m.Cursor())
	}
}

// TestEnterToggleReanchorsTracked verifies the collapse/expand path re-anchors
// the tracked selection so a following data refresh keeps the cursor put.
func TestEnterToggleReanchorsTracked(t *testing.T) {
	now := time.Now()
	m := NewSidebar(40, 20)
	sessions := []*session.Session{
		{ID: "s1", TmuxSession: "grp", DisplayName: "one", LastActivity: now},
		{ID: "s2", TmuxSession: "grp", DisplayName: "two", LastActivity: now},
	}
	m.SetSessions(sessions)

	// Move cursor to the group header (index 0).
	m.SetCursor(0)
	if m.SelectedGroupName() != "grp" {
		t.Fatalf("setup: cursor not on group header, got %q", m.SelectedGroupName())
	}

	// Collapse the group via Enter.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// A subsequent identical data refresh must not move the cursor off the
	// header (updateTracked recorded the group as the tracked selection).
	m.SetSessions(sessions)
	if got := m.SelectedGroupName(); got != "grp" {
		t.Errorf("after refresh cursor moved off header: SelectedGroupName=%q, want grp", got)
	}
}
