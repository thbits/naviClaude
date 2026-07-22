package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// newTestModel builds a minimal Model with a sidebar and a temp-backed view
// store, bypassing New() (which touches tmux and the real config path).
func newTestModel(t *testing.T, cfg config.Config, stored session.ViewState) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "view-state.json")
	store := session.NewViewStateStore(path)
	if err := store.Save(stored); err != nil {
		t.Fatal(err)
	}
	m := Model{
		sidebar:   ui.NewSidebar(30, 24),
		preview:   ui.NewPreview(50, 24),
		cfg:       cfg,
		viewState: store,
	}
	return m
}

func viewStateTestSessions() []*session.Session {
	return []*session.Session{
		{ID: "first", TmuxSession: "aaa", TmuxTarget: "aaa:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now()},
		{ID: "second", TmuxSession: "bbb", TmuxTarget: "bbb:1.0", ProjectName: "p2", Status: session.StatusActive, LastActivity: time.Now()},
	}
}

func TestRestoreSelectsStoredSession(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.FocusLastSession = true
	m := newTestModel(t, cfg, session.ViewState{LastSessionID: "second"})
	m.sidebar.SetSessions(viewStateTestSessions())

	_ = m.maybeRestoreLastSession()

	sel := m.sidebar.SelectedSession()
	if sel == nil || sel.ID != "second" {
		t.Fatalf("restore should select stored session; got %v", sel)
	}
}

func TestRestoreFallsBackWhenSessionGone(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.FocusLastSession = true
	m := newTestModel(t, cfg, session.ViewState{LastSessionID: "ghost"})
	m.sidebar.SetSessions(viewStateTestSessions())

	_ = m.maybeRestoreLastSession()

	sel := m.sidebar.SelectedSession()
	if sel == nil || sel.ID != "first" {
		t.Fatalf("missing stored session should leave first-session cursor; got %v", sel)
	}
	if m.restoredLastSession {
		t.Error("guard must stay false when nothing was restored, so a later scan can retry")
	}
}

func TestRestoreDisabledByConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.FocusLastSession = false // explicitly disabled
	m := newTestModel(t, cfg, session.ViewState{LastSessionID: "second"})
	m.sidebar.SetSessions(viewStateTestSessions())

	_ = m.maybeRestoreLastSession()

	sel := m.sidebar.SelectedSession()
	if sel == nil || sel.ID != "first" {
		t.Fatalf("disabled restore should leave first-session cursor; got %v", sel)
	}
}

func TestPersistViewStateWritesSelection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.FocusLastSession = true
	m := newTestModel(t, cfg, session.ViewState{})
	m.sidebar.SetSessions(viewStateTestSessions())
	m.sidebar.SelectByID("second")

	m.persistViewState()

	if got := m.viewState.Get().LastSessionID; got != "second" {
		t.Fatalf("persisted LastSessionID = %q, want %q", got, "second")
	}
}
