package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// newFocusModel builds a Model with a real sidebar/preview/statusbar so the
// new-session focus path can be exercised end to end.
func newFocusModel(collapseAfterHrs float64, sessions []*session.Session) Model {
	sb := ui.NewSidebar(40, 20)
	if collapseAfterHrs > 0 {
		sb.SetCollapseAfterHours(collapseAfterHrs)
	}
	sb.SetSessions(sessions)
	return Model{
		sidebar:   sb,
		preview:   ui.NewPreview(40, 20),
		statusbar: ui.NewStatusBar(80, "test"),
		sessions:  sessions,
	}
}

// Creating a new session inside a STALE group (which auto-collapses) must still
// move the cursor onto the new session. The placeholder previously carried a
// zero LastActivity, so its group auto-collapsed and the new row was hidden --
// leaving focus on the old cursor position.
func TestEnterNewSessionPassthroughFocusesInStaleGroup(t *testing.T) {
	stale := time.Now().Add(-5 * time.Hour)
	m := newFocusModel(1, []*session.Session{
		{ID: "old", TmuxSession: "myproj", TmuxTarget: "myproj:0.0", CWD: "/home/u/myproj", LastActivity: stale, Status: session.StatusActive},
	})

	placeholder := &session.Session{
		TmuxSession:  "myproj",
		TmuxTarget:   "myproj:1.0",
		CWD:          "/home/u/myproj",
		ProjectName:  "claude",
		Status:       session.StatusActive,
		LastActivity: time.Now(),
	}
	m.enterNewSessionPassthrough(placeholder)

	sel := m.sidebar.SelectedSession()
	if sel == nil {
		t.Fatalf("no session selected; cursor did not land on the new session")
	}
	if sel.TmuxTarget != "myproj:1.0" {
		t.Errorf("selected target = %q, want myproj:1.0", sel.TmuxTarget)
	}
	if m.mode != ModePassthrough {
		t.Errorf("mode = %v, want passthrough", m.mode)
	}
	if m.pendingNewTarget != "myproj:1.0" {
		t.Errorf("pendingNewTarget = %q, want myproj:1.0", m.pendingNewTarget)
	}
}

// Creating a new session inside a group the user manually collapsed must
// re-expand it and focus the new session. LastActivity alone can't fix this
// case because userToggled groups are exempt from auto-collapse either way.
func TestEnterNewSessionPassthroughFocusesInUserCollapsedGroup(t *testing.T) {
	m := newFocusModel(0, []*session.Session{
		{ID: "old", TmuxSession: "myproj", TmuxTarget: "myproj:0.0", CWD: "/home/u/myproj", LastActivity: time.Now(), Status: session.StatusActive},
	})
	// User collapses the group by toggling its header (enter on the group row).
	m.sidebar.SetCursor(0) // group header
	updated, _ := m.sidebar.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.sidebar = updated
	if m.sidebar.SelectedSession() != nil {
		t.Fatalf("precondition: group should be collapsed (header selected)")
	}

	placeholder := &session.Session{
		TmuxSession:  "myproj",
		TmuxTarget:   "myproj:1.0",
		CWD:          "/home/u/myproj",
		ProjectName:  "claude",
		Status:       session.StatusActive,
		LastActivity: time.Now(),
	}
	m.enterNewSessionPassthrough(placeholder)

	sel := m.sidebar.SelectedSession()
	if sel == nil {
		t.Fatalf("no session selected; cursor did not land on the new session")
	}
	if sel.TmuxTarget != "myproj:1.0" {
		t.Errorf("selected target = %q, want myproj:1.0", sel.TmuxTarget)
	}
}
