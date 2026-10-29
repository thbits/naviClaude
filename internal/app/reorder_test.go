package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// fakeMover records the tmux moves and renames a drag or menu asks for.
type fakeMover struct{ calls []string }

func (f *fakeMover) MoveWindow(src, dst string, before bool) error {
	where := "after"
	if before {
		where = "before"
	}
	f.calls = append(f.calls, "move "+src+" "+where+" "+dst)
	return nil
}

func (f *fakeMover) MoveWindowToSession(src, name string) error {
	f.calls = append(f.calls, "move "+src+" to "+name)
	return nil
}

func (f *fakeMover) RenameSession(oldName, newName string) error {
	f.calls = append(f.calls, "rename "+oldName+" "+newName)
	return nil
}

// dragModel has groups "aa" (one, two) and "bb" (three) under name sort. The
// sessions are subprocesses so selecting them never resizes a real tmux window.
func dragModel(t *testing.T) (*Model, *fakeMover, *config.Config) {
	t.Helper()
	now := time.Now()
	sessions := []*session.Session{
		{ID: "s1", DisplayName: "one", TmuxSession: "aa", TmuxTarget: "aa:1.0", TmuxPaneID: "%1", Status: session.StatusIdle, LastActivity: now, Subprocess: true},
		{ID: "s2", DisplayName: "two", TmuxSession: "aa", TmuxTarget: "aa:2.0", TmuxPaneID: "%2", Status: session.StatusIdle, LastActivity: now, Subprocess: true},
		{ID: "s3", DisplayName: "three", TmuxSession: "bb", TmuxTarget: "bb:1.0", TmuxPaneID: "%3", Status: session.StatusIdle, LastActivity: now, Subprocess: true},
	}
	m := newFocusModel(0, sessions)
	m.width, m.height = 100, 30
	m.sidebarWidthPct = 30
	m.keys = DefaultKeyMap()
	m.search = ui.NewSearch()
	m.renameInput = ui.NewRenameInput()
	m.contextMenu = ui.NewContextMenu()
	m.resumePicker = ui.NewSessionPicker()
	m.allSessions = sessions
	m.cfg = config.DefaultConfig()
	m.resizeComponents()
	m.sidebar.SetBreathingFrame(0)
	m.mode = ModeList
	mover := &fakeMover{}
	m.mover = mover
	saved := &config.Config{}
	m.saveConfig = func(c config.Config) error { *saved = c; return nil }
	m.now = time.Now
	return &m, mover, saved
}

// releaseRun releases the left button and runs the returned command's message.
func releaseRun(m *Model, x, y int) tea.Msg {
	cmd := releaseCmd(m, x, y)
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestDragRowAboveAnotherMovesItsWindow(t *testing.T) {
	m, mover, saved := dragModel(t)
	press(m, tea.MouseButtonLeft, xList, rowOf(t, m, "s2"))
	motion(m, xList, rowOf(t, m, "s1")) // top line of "one": land before it
	if got := ansiFree(m.sidebar.View()); !strings.Contains(got, "━") {
		t.Errorf("no drop line while dragging:\n%s", got)
	}
	msg := releaseRun(m, xList, rowOf(t, m, "s1"))
	if _, ok := msg.(windowMovedMsg); !ok {
		t.Fatalf("drop returned %T, want windowMovedMsg", msg)
	}
	if want := []string{"move %2 before %1"}; strings.Join(mover.calls, ";") != strings.Join(want, ";") {
		t.Errorf("tmux calls = %v, want %v", mover.calls, want)
	}
	if m.cfg.SessionSortOrder != "manual" || saved.SessionSortOrder != "manual" {
		t.Errorf("session sort = %q (saved %q), want manual", m.cfg.SessionSortOrder, saved.SessionSortOrder)
	}
	if strings.Contains(ansiFree(m.sidebar.View()), "━") {
		t.Error("drop line still drawn after release")
	}
}

func TestDragRowOntoGroupHeaderMovesToThatSession(t *testing.T) {
	m, mover, _ := dragModel(t)
	press(m, tea.MouseButtonLeft, xList, rowOf(t, m, "s1"))
	y := viewRow(t, m, "bb")
	motion(m, xList, y)
	releaseRun(m, xList, y)
	if want := "move %1 to bb"; strings.Join(mover.calls, ";") != want {
		t.Errorf("tmux calls = %v, want [%s]", mover.calls, want)
	}
}

func TestPressWithoutLeavingRowMovesNothing(t *testing.T) {
	m, mover, _ := dragModel(t)
	y := rowOf(t, m, "s2")
	press(m, tea.MouseButtonLeft, xList, y)
	motion(m, xList+3, y)
	releaseRun(m, xList+3, y)
	if len(mover.calls) != 0 || m.cfg.SessionSortOrder != "name" {
		t.Errorf("a click moved something: calls %v, sort %q", mover.calls, m.cfg.SessionSortOrder)
	}
}

func TestDragGroupHeaderReordersGroups(t *testing.T) {
	m, mover, saved := dragModel(t)
	press(m, tea.MouseButtonLeft, xList, viewRow(t, m, "bb"))
	y := viewRow(t, m, "aa")
	motion(m, xList, y)
	releaseRun(m, xList, y)
	if got := strings.Join(m.sidebar.GroupOrder(), ","); got != "bb,aa" {
		t.Errorf("group order = %s, want bb,aa", got)
	}
	if saved.GroupSortOrder != "manual" {
		t.Errorf("group sort saved as %q, want manual", saved.GroupSortOrder)
	}
	if len(mover.calls) != 0 {
		t.Errorf("group reorder ran tmux: %v", mover.calls)
	}
	if viewRow(t, m, "bb") > viewRow(t, m, "aa") {
		t.Error("bb still renders below aa")
	}
}

func TestRenameKeyOnGroupHeaderRenamesTmuxSession(t *testing.T) {
	m, mover, _ := dragModel(t)
	m.sidebar.SetCursor(0) // the "aa" header
	m.sidebar.ToggleGroupAtCursor()
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	*m = next.(Model)
	if m.mode != ModeRenameSession || m.renameInput.Value() != "aa" {
		t.Fatalf("mode %v value %q, want rename prefilled with aa", m.mode, m.renameInput.Value())
	}
	m.renameInput.ActivateWithValue("work")
	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = next.(Model)
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	msg := cmd().(windowMovedMsg)
	if strings.Join(mover.calls, ";") != "rename aa work" {
		t.Errorf("tmux calls = %v, want rename aa work", mover.calls)
	}
	m.applyWindowMoved(msg)
	if !m.sidebar.IsCollapsed("work") {
		t.Error("collapsed state did not follow the rename")
	}
}

func TestRenameGroupRejectsTargetSeparators(t *testing.T) {
	m, mover, _ := dragModel(t)
	if cmd := m.renameGroupCmd("aa", "a:b"); cmd != nil {
		t.Error("a name with ':' was sent to tmux")
	}
	if len(mover.calls) != 0 {
		t.Errorf("tmux calls = %v", mover.calls)
	}
}

func TestRightClickGroupHeaderOpensGroupMenu(t *testing.T) {
	m, _, _ := dragModel(t)
	press(m, tea.MouseButtonRight, xList, viewRow(t, m, "bb"))
	if !m.contextMenu.IsVisible() || m.contextMenu.Group() != "bb" {
		t.Fatalf("group menu not open for bb (visible %v, group %q)", m.contextMenu.IsVisible(), m.contextMenu.Group())
	}
	if a := m.contextMenu.SelectedAction(); a != "rename_group" {
		t.Errorf("first item = %q, want rename_group", a)
	}
	next, _ := m.runContextMenuSelection()
	*m = next.(Model)
	if m.mode != ModeRenameSession || m.renameGroup != "bb" {
		t.Errorf("mode %v renameGroup %q, want renaming bb", m.mode, m.renameGroup)
	}
}

func ansiFree(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestDragClosedHeaderMovesNothing(t *testing.T) {
	m, _, saved := dragModel(t)
	m.sessions = append(m.sessions, &session.Session{ID: "c1", DisplayName: "old", Status: session.StatusClosed, LastActivity: time.Now()})
	m.sidebar.SetSessions(m.sessions)
	press(m, tea.MouseButtonLeft, xList, viewRow(t, m, ui.ClosedGroupName))
	y := viewRow(t, m, "aa")
	motion(m, xList, y)
	releaseRun(m, xList, y)
	if len(m.sidebar.GroupOrder()) != 0 || saved.GroupSortOrder != "" {
		t.Errorf("Closed drag changed the order: %v, saved sort %q", m.sidebar.GroupOrder(), saved.GroupSortOrder)
	}
}

func TestLongPressOnMovableRowStillOpensMenu(t *testing.T) {
	m, mover, _ := dragModel(t)
	clk := time.Now()
	m.now = func() time.Time { return clk }
	y := rowOf(t, m, "s2")
	press(m, tea.MouseButtonLeft, xList, y)
	clk = clk.Add(longPressThreshold)
	releaseRun(m, xList, y)
	if !m.contextMenu.IsVisible() {
		t.Error("long-press on a draggable row opened no menu")
	}
	if len(mover.calls) != 0 {
		t.Errorf("long-press moved a window: %v", mover.calls)
	}
}

func TestCancelledDragBackOnItsRowOpensNoMenu(t *testing.T) {
	m, mover, _ := dragModel(t)
	clk := time.Now()
	m.now = func() time.Time { return clk }
	y := rowOf(t, m, "s2")
	press(m, tea.MouseButtonLeft, xList, y)
	motion(m, xList, rowOf(t, m, "s1"))
	motion(m, xList, y) // back over itself: no valid drop
	clk = clk.Add(longPressThreshold)
	releaseRun(m, xList, y)
	if m.contextMenu.IsVisible() || len(mover.calls) != 0 {
		t.Errorf("menu %v, tmux calls %v; want neither", m.contextMenu.IsVisible(), mover.calls)
	}
}

func TestMoveKeyOpensMovePickerForSelectedSession(t *testing.T) {
	m, _, _ := dragModel(t)
	m.sidebar.SetCursor(0) // the "aa" header: nothing to move
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	if got := next.(Model).mode; got != ModeList {
		t.Errorf("m on a header: mode %v, want list", got)
	}
	m.sidebar.SelectByID("s2")
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	got := next.(Model)
	if got.mode != ModeResumePicker || got.pendingMoveSession == nil || got.pendingMoveSession.ID != "s2" {
		t.Errorf("m on a row: mode %v, pending %v; want the move picker for s2", got.mode, got.pendingMoveSession)
	}
}

func TestPinKeyPinsAndPersists(t *testing.T) {
	m, _, _ := dragModel(t)
	m.viewState = session.NewViewStateStore(t.TempDir() + "/view-state.json")
	m.sidebar.SelectByID("s2")
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	*m = next.(Model)
	if got := m.viewState.Get().Pinned; len(got) != 1 || got[0] != "s2" {
		t.Fatalf("saved pins = %v, want [s2]", got)
	}
	if rowOf(t, m, "s2") > rowOf(t, m, "s1") {
		t.Error("pinned s2 still renders below s1")
	}
	if sel := m.sidebar.SelectedSession(); sel == nil || sel.ID != "s2" {
		t.Errorf("selection moved off the pinned row to %v", sel)
	}
}
