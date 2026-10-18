package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

func TestSetAsUnreadSurvivesSeenClearUntilPreviewMoves(t *testing.T) {
	m := newTestModel(t, config.DefaultConfig(), session.ViewState{})
	sessions := []*session.Session{
		{ID: "a", TmuxTarget: "s:1.0", TmuxPaneID: "%1", ProjectName: "p", Status: session.StatusIdle},
	}
	m.sidebar.SetSessions(sessions)
	m.activeSessions = sessions
	m.previewTarget = "s:1.0"

	next, cmd := m.executeContextAction("mark_unread", sessions[0])
	m = next.(Model)
	if cmd == nil {
		t.Fatal("mark_unread returned no command to set the marker")
	}
	if sessions[0].Attention != attention.Done {
		t.Fatalf("attention = %q, want %q right away", sessions[0].Attention, attention.Done)
	}
	if c := m.clearSeenAttentionCmd(); c != nil {
		t.Error("the refresh cleared the marker while the session is still selected")
	}

	m.selectPreviewSession(nil) // the user moves on
	if c := m.clearSeenAttentionCmd(); c == nil {
		t.Error("the marker should clear once the session is viewed again")
	}
}

func TestSetAsUnreadReleasedByPassthrough(t *testing.T) {
	m := newTestModel(t, config.DefaultConfig(), session.ViewState{})
	sessions := []*session.Session{
		{ID: "a", TmuxTarget: "s:1.0", TmuxPaneID: "%1", ProjectName: "p", Status: session.StatusIdle},
	}
	m.sidebar.SetSessions(sessions)
	m.activeSessions = sessions
	m.previewTarget = "s:1.0"

	next, _ := m.executeContextAction("mark_unread", sessions[0])
	m = next.(Model)
	m.setFocus(ModePassthrough)
	if c := m.clearSeenAttentionCmd(); c == nil {
		t.Error("entering passthrough should release the hold so the marker clears")
	}
}

func TestBranchForPrefersLiveBranchOnlyForOpenSessions(t *testing.T) {
	m := newTestModel(t, config.DefaultConfig(), session.ViewState{})
	m.sidebar.SetBranch("/repo", session.GitRef{Repo: "repo", Branch: "now"}, true)

	live := &session.Session{Status: session.StatusIdle, CWD: "/repo", GitBranch: "then"}
	if got := m.branchFor(live); got != "now" {
		t.Errorf("live branch = %q, want the current one", got)
	}
	closed := &session.Session{Status: session.StatusClosed, CWD: "/repo", GitBranch: "then"}
	if got := m.branchFor(closed); got != "then" {
		t.Errorf("closed branch = %q, want the one it worked on", got)
	}
	closed.GitBranch = ""
	if got := m.branchFor(closed); got != "now" {
		t.Errorf("closed branch without history = %q, want the current one", got)
	}
	m.sidebar.SetBranch("/repo", session.GitRef{Repo: "repo", Branch: "detached"}, true)
	if got := m.branchFor(live); got != "" {
		t.Errorf("detached HEAD should hide the item, got %q", got)
	}
}

func TestOpenInEditorLaunchesGUIEditorWithoutTmux(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor = "code --wait"
	m := newTestModel(t, cfg, session.ViewState{}) // no tmux client: a tmux call would panic
	sess := &session.Session{Status: session.StatusIdle, CWD: t.TempDir(), TmuxSession: "s"}
	_, cmd := m.openInEditor(sess)
	if cmd == nil {
		t.Fatal("GUI editor should be launched by a command, not a tmux window")
	}
}

func TestHoverMovesContextMenuCursor(t *testing.T) {
	m := newTestModel(t, config.DefaultConfig(), session.ViewState{})
	m.width, m.height = 120, 40
	sess := &session.Session{ID: "a", TmuxTarget: "s:1.0", Status: session.StatusActive, CWD: "/repo"}
	m.contextMenu.SetTermSize(m.width, m.height)
	m.contextMenu.Show(10, 5, sess, ui.MenuCaps{})
	m.mode = ModeContextMenu
	if m.contextMenu.SelectedAction() != "focus" {
		t.Fatalf("menu should open on focus, got %q", m.contextMenu.SelectedAction())
	}

	x, y, _, _ := m.contextMenu.Rect()
	next, _ := m.handleMouse(tea.MouseMsg{X: x + 2, Y: y + 2, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	m = next.(Model)
	if got := m.contextMenu.SelectedAction(); got != "jump" {
		t.Errorf("hovering the second item selected %q, want jump", got)
	}
	if !m.contextMenu.IsVisible() {
		t.Error("hover must not close the menu")
	}
}
