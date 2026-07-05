package app

import (
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// newSidebarWith builds a sidebar populated with the given sessions. When
// collapseAfterHrs > 0, stale groups auto-collapse (matching the running app).
func newSidebarWith(collapseAfterHrs float64, sessions []*session.Session) ui.SidebarModel {
	sb := ui.NewSidebar(40, 20)
	if collapseAfterHrs > 0 {
		sb.SetCollapseAfterHours(collapseAfterHrs)
	}
	sb.SetSessions(sessions)
	return sb
}

// When the cursor sits on a collapsed group header, a new session must target
// that group's tmux session -- not naviClaude's own session. The group name is
// the tmux session name, so it resolves even though the collapsed group has no
// visible child rows to scan.
func TestResolveNewSessionTargetCollapsedGroupHeader(t *testing.T) {
	stale := time.Now().Add(-2 * time.Hour)
	sb := newSidebarWith(1, []*session.Session{
		{ID: "a", TmuxSession: "myproj", TmuxTarget: "myproj:0.0", CWD: "/home/u/myproj", LastActivity: stale},
	})
	// Cursor on the (collapsed) group header, which is the only flat row.
	sb.SetCursor(0)
	if sb.SelectedSession() != nil {
		t.Fatalf("precondition: cursor should be on a group header, not a session")
	}

	m := Model{sidebar: sb, currentTmuxSession: "navi"}
	tmuxSess, cwd := m.resolveNewSessionTarget()

	if tmuxSess != "myproj" {
		t.Errorf("tmuxSess = %q, want %q", tmuxSess, "myproj")
	}
	if cwd != "/home/u/myproj" {
		t.Errorf("cwd = %q, want %q", cwd, "/home/u/myproj")
	}
}

// A hovered session takes precedence: its own tmux session and cwd are used.
func TestResolveNewSessionTargetHoveredSession(t *testing.T) {
	sb := newSidebarWith(0, []*session.Session{
		{ID: "a", TmuxSession: "myproj", TmuxTarget: "myproj:0.0", CWD: "/home/u/myproj", LastActivity: time.Now()},
	})
	if ok := sb.SelectByID("a"); !ok {
		t.Fatalf("precondition: could not select session a")
	}

	m := Model{sidebar: sb, currentTmuxSession: "navi"}
	tmuxSess, cwd := m.resolveNewSessionTarget()

	if tmuxSess != "myproj" || cwd != "/home/u/myproj" {
		t.Errorf("got (%q, %q), want (myproj, /home/u/myproj)", tmuxSess, cwd)
	}
}

// A hovered closed session has no tmux session of its own (only live pane
// detection assigns one), so the new session must fall back to naviClaude's
// own tmux session while keeping the closed session's cwd for the dir picker.
func TestResolveNewSessionTargetHoveredClosedSessionFallsBack(t *testing.T) {
	sb := newSidebarWith(0, []*session.Session{
		{ID: "c", Status: session.StatusClosed, CWD: "/home/u/old", LastActivity: time.Now().Add(-3 * time.Hour)},
	})
	sb.ExpandAll()
	if ok := sb.SelectByID("c"); !ok {
		t.Fatalf("precondition: could not select closed session c")
	}

	m := Model{sidebar: sb, currentTmuxSession: "navi"}
	tmuxSess, cwd := m.resolveNewSessionTarget()

	if tmuxSess != "navi" {
		t.Errorf("tmuxSess = %q, want %q (fallback to current)", tmuxSess, "navi")
	}
	if cwd != "/home/u/old" {
		t.Errorf("cwd = %q, want %q (keep the closed session's cwd)", cwd, "/home/u/old")
	}
}

// The "Closed" group is not a real tmux session, so selecting its header must
// fall through to naviClaude's own tmux session.
func TestResolveNewSessionTargetClosedGroupFallsBack(t *testing.T) {
	sb := newSidebarWith(0, []*session.Session{
		{ID: "c", Status: session.StatusClosed, CWD: "/home/u/old", LastActivity: time.Now().Add(-3 * time.Hour)},
	})
	// The Closed group is collapsed by default, so its header is the only row.
	sb.SetCursor(0)
	if got := sb.SelectedGroupName(); got != "Closed" {
		t.Fatalf("precondition: selected group = %q, want Closed", got)
	}

	m := Model{sidebar: sb, currentTmuxSession: "navi"}
	tmuxSess, _ := m.resolveNewSessionTarget()

	if tmuxSess != "navi" {
		t.Errorf("tmuxSess = %q, want %q (fallback to current)", tmuxSess, "navi")
	}
}
