package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// statusSessions covers every status: group aa (waiting, done, working, idle),
// group bb (waiting, done, idle) and a closed session.
func statusSessions() []*session.Session {
	now := time.Now()
	mk := func(n int, id, group string, st session.SessionStatus, att string) *session.Session {
		// One shared window, naviClaude's own, so selecting never resizes a real tmux window.
		return &session.Session{ID: id, ProjectName: id, TmuxSession: group, TmuxTarget: fmt.Sprintf("zz:0.%d", n),
			TmuxPaneID: fmt.Sprintf("%%%d", n), Status: st, Attention: att, LastActivity: now}
	}
	return []*session.Session{
		mk(1, "a-wait", "aa", session.StatusWaiting, ""),
		mk(2, "a-done", "aa", session.StatusIdle, attention.Done),
		mk(3, "a-work", "aa", session.StatusActive, ""),
		mk(4, "a-idle", "aa", session.StatusIdle, ""),
		mk(5, "b-wait", "bb", session.StatusWaiting, ""),
		mk(6, "b-done", "bb", session.StatusIdle, attention.Done),
		mk(7, "b-idle", "bb", session.StatusIdle, ""),
		{ID: "closed", ProjectName: "closed", Status: session.StatusClosed, LastActivity: now.Add(-time.Hour)},
	}
}

func statusModel(t *testing.T, sessions []*session.Session) *Model {
	t.Helper()
	m := newFocusModel(0, sessions)
	m.width, m.height = 120, 30
	m.currentTmuxWin = "zz:0"
	m.currentTmuxSession = "work"
	m.keys = DefaultKeyMap()
	m.search = ui.NewSearch()
	m.allSessions = sessions
	m.aliasStore = session.NewAliasStore(filepath.Join(t.TempDir(), "aliases.json"))
	m.mode = ModeList
	m.resizeComponents()
	return &m
}

func pressKey(m *Model, msg tea.KeyMsg) tea.Cmd {
	next, cmd := m.handleKey(msg)
	*m = next.(Model)
	return cmd
}

// listed is the session IDs shown in the sidebar, top to bottom.
func listed(m *Model) string {
	var ids []string
	for _, it := range m.sidebar.FlatItems() {
		if !it.IsGroup {
			ids = append(ids, it.Session.ID)
		}
	}
	return strings.Join(ids, " ")
}

func TestNextAttentionKeyCyclesWaitingThenDone(t *testing.T) {
	m := statusModel(t, statusSessions())
	m.sidebar.SelectByID("b-idle") // outside the ring: the first press goes to its start
	var visited []string
	for range 5 {
		typeKeys(t, m, " ")
		visited = append(visited, selectedID(m))
	}
	if got, want := strings.Join(visited, " "), "a-wait b-wait a-done b-done a-wait"; got != want {
		t.Errorf("visited %s, want %s", got, want)
	}
	if !m.selectionPending {
		t.Error("the move should sync the panes like any other navigation")
	}
}

func TestNextAttentionKeyFallsBackToWorking(t *testing.T) {
	sessions := statusSessions()
	for _, s := range sessions {
		if s.Status == session.StatusWaiting {
			s.Status = session.StatusActive
		}
		s.Attention = ""
	}
	m := statusModel(t, sessions)
	m.sidebar.SelectByID("a-idle")
	var visited []string
	for range 4 {
		typeKeys(t, m, " ")
		visited = append(visited, selectedID(m))
	}
	// Both groups list their working rows by name.
	if got, want := strings.Join(visited, " "), "a-wait a-work b-wait a-wait"; got != want {
		t.Errorf("visited %s, want the working sessions %s", got, want)
	}
}

func TestNextAttentionKeyExpandsCollapsedGroup(t *testing.T) {
	sessions := statusSessions()
	sessions[0].Status = session.StatusIdle // only bb still waits
	m := statusModel(t, sessions)
	for i, it := range m.sidebar.FlatItems() {
		if it.IsGroup && m.sidebar.SelectedGroupName() != "bb" {
			m.sidebar.SetCursor(i)
		}
	}
	if m.sidebar.SelectedGroupName() != "bb" {
		t.Fatalf("precondition: cursor should be on the bb header")
	}
	m.sidebar.ToggleGroupAtCursor()
	if strings.Contains(listed(m), "b-wait") {
		t.Fatalf("precondition: bb should be collapsed, rows = %s", listed(m))
	}
	typeKeys(t, m, " ")
	if got := selectedID(m); got != "b-wait" {
		t.Errorf("selected = %q, want b-wait in the re-opened group", got)
	}
}

func TestNextAttentionKeyNoticeWhenNothingToGoTo(t *testing.T) {
	sessions := statusSessions()[6:] // idle and closed
	m := statusModel(t, sessions)
	before := selectedID(m)
	typeKeys(t, m, " ")
	if selectedID(m) != before {
		t.Errorf("selection moved to %q with nothing to go to", selectedID(m))
	}
	if line := statusLine(m); !strings.Contains(line, "no session is waiting, done or working") {
		t.Errorf("status line = %q, want a notice", line)
	}

	// Already on the only candidate.
	m = statusModel(t, statusSessions()[:1])
	m.sidebar.SelectByID("a-wait")
	typeKeys(t, m, " ")
	if line := statusLine(m); !strings.Contains(line, "no other session") {
		t.Errorf("status line = %q, want a notice that nothing else needs attention", line)
	}
}

func TestStatusFilterKeysToggleOneFilterAtATime(t *testing.T) {
	m := statusModel(t, statusSessions())
	all := listed(m)
	for _, tc := range []struct{ key, want string }{
		{"w", "a-wait b-wait"},
		{"d", "a-done b-done"},
		{"a", "a-work"},
		{"i", "a-idle b-idle"},
	} {
		typeKeys(t, m, tc.key)
		if got := listed(m); got != tc.want {
			t.Errorf("after %q: rows = %s, want %s", tc.key, got, tc.want)
		}
	}
	typeKeys(t, m, "i") // the active filter's key clears it
	if got := listed(m); got != all {
		t.Errorf("pressing the filter key again: rows = %s, want all (%s)", got, all)
	}
	typeKeys(t, m, "w", "esc")
	if got := listed(m); got != all || m.sidebar.StatusFilter() != ui.FilterNone {
		t.Errorf("after Esc: rows = %s, filter = %q; want the filter cleared", got, m.sidebar.StatusFilter())
	}
}

func TestStatusFilterTagInTitleBar(t *testing.T) {
	m := statusModel(t, statusSessions())
	if bar := ansi.Strip(m.renderTitleBar()); strings.Contains(bar, "filter:") {
		t.Fatalf("unfiltered title bar shows a filter tag: %q", bar)
	}
	typeKeys(t, m, "d")
	bar := ansi.Strip(m.renderTitleBar())
	if !strings.Contains(bar, "filter: done") {
		t.Errorf("title bar = %q, want the active filter named", bar)
	}
	// The summary still counts every session, filtered or not.
	if !strings.Contains(bar, "2 needs you") {
		t.Errorf("title bar = %q, want the unfiltered status counts", bar)
	}
	for _, width := range []int{120, 60, 30, 20} {
		m.width = width
		if b := m.renderTitleBar(); lipgloss.Height(b) != 1 || lipgloss.Width(b) > width {
			t.Errorf("width %d: title bar is %dx%d", width, lipgloss.Width(b), lipgloss.Height(b))
		}
	}
}

func TestStatusFilterKeepsSelectionAndSyncsWhenItMoves(t *testing.T) {
	m := statusModel(t, statusSessions())
	m.sidebar.SelectByID("b-done")
	typeKeys(t, m, "d")
	if got := selectedID(m); got != "b-done" {
		t.Fatalf("selected = %q, want b-done kept under its own filter", got)
	}
	typeKeys(t, m, "w") // b-done is hidden now
	if got := selectedID(m); got != "a-wait" {
		t.Fatalf("selected = %q, want the first waiting row", got)
	}
	if m.selectionPending {
		t.Error("the panes should already follow the row the filter moved the cursor to")
	}
	typeKeys(t, m, "esc")
	if got := selectedID(m); got != "a-wait" {
		t.Errorf("selected = %q after clearing, want a-wait", got)
	}
}

// A refresh that marks the selected done session seen must not drop it from
// under the cursor, or the filter would clear one done row after another.
func TestStatusFilterDoneRowSurvivesBeingSeen(t *testing.T) {
	m := statusModel(t, statusSessions())
	typeKeys(t, m, "d")
	m.sidebar.SelectByID("a-done")

	seen := statusSessions()
	seen[1].Attention = ""
	m.applySessionRefresh(sessionRefreshMsg{active: seen[:7], closed: seen[7:]})
	if got, want := listed(m), "a-done b-done"; got != want {
		t.Fatalf("rows = %s, want %s", got, want)
	}
	if got := selectedID(m); got != "a-done" {
		t.Errorf("selected = %q, want a-done", got)
	}
}

func TestStatusFilterEmptyListClearsPreview(t *testing.T) {
	m := statusModel(t, statusSessions()[6:]) // idle and closed only
	m.sidebar.SelectByID("b-idle")
	m.afterSelectionChange()
	m.preview.SetContent("pane text")
	typeKeys(t, m, "w")
	if got := listed(m); got != "" {
		t.Fatalf("rows = %s, want none", got)
	}
	view := ansi.Strip(m.preview.View())
	if strings.Contains(view, "pane text") || !strings.Contains(view, "No session selected") {
		t.Errorf("preview still shows a session the filter hides:\n%s", view)
	}
	typeKeys(t, m, "w")
	if selectedID(m) == "" {
		t.Error("clearing the filter should select a session again")
	}
}

// Search lists every session whatever the filter, which comes back on Esc.
func TestSearchIgnoresStatusFilter(t *testing.T) {
	m := statusModel(t, statusSessions())
	typeKeys(t, m, "w", "/")
	if m.mode != ModeSearch {
		t.Fatalf("mode = %v, want search", m.mode)
	}
	if got := listed(m); !strings.Contains(got, "a-idle") || !strings.Contains(got, "closed") {
		t.Errorf("search rows = %s, want every session", got)
	}
	if bar := ansi.Strip(m.renderTitleBar()); strings.Contains(bar, "filter:") {
		t.Errorf("title bar shows the filter tag while search overrides it: %q", bar)
	}
	typeKeys(t, m, "esc")
	if got, want := listed(m), "a-wait b-wait"; got != want {
		t.Errorf("rows after search = %s, want the filter back (%s)", got, want)
	}
}

// Opening a search result the filter would hide drops the filter instead of
// acting on some other row.
func TestSearchResultOutsideFilterClearsIt(t *testing.T) {
	m := statusModel(t, statusSessions())
	typeKeys(t, m, "w", "/")
	m.sidebar.SelectByID("a-idle")
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := selectedID(m); got != "a-idle" {
		t.Fatalf("selected = %q, want the chosen result a-idle", got)
	}
	if m.sidebar.StatusFilter() != ui.FilterNone {
		t.Errorf("filter = %q, want it cleared", m.sidebar.StatusFilter())
	}
	if m.mode != ModePassthrough {
		t.Errorf("mode = %v, want passthrough on the chosen session", m.mode)
	}
}

func TestNewSessionClearsStatusFilter(t *testing.T) {
	m := statusModel(t, statusSessions())
	typeKeys(t, m, "w")
	m.enterNewSessionPassthrough(&session.Session{TmuxSession: "aa", TmuxTarget: "zz:0.9", Status: session.StatusActive, LastActivity: time.Now()})
	if sel := m.sidebar.SelectedSession(); sel == nil || sel.TmuxTarget != "zz:0.9" {
		t.Errorf("selected = %+v, want the new session", sel)
	}
	if m.sidebar.StatusFilter() != ui.FilterNone {
		t.Error("the filter should be cleared so the new session is listed")
	}
}

func TestNextAttentionKeyStaysInsideStatusFilter(t *testing.T) {
	m := statusModel(t, statusSessions())
	typeKeys(t, m, "d", " ", " ")
	if got := selectedID(m); got != "a-done" && got != "b-done" {
		t.Errorf("selected = %q, want a done session", got)
	}
	typeKeys(t, m, "i", " ")
	if line := statusLine(m); !strings.Contains(line, "filter: idle") {
		t.Errorf("status line = %q, want a notice naming the filter", line)
	}
}

// D opens the detail popup; d is the done filter.
func TestDetailOpensOnShiftD(t *testing.T) {
	m := statusModel(t, statusSessions())
	m.sidebar.SelectByID("a-idle")
	typeKeys(t, m, "D")
	if m.mode != ModeDetail || !m.detail.IsVisible() {
		t.Errorf("mode = %v, detail visible = %v; want the detail popup", m.mode, m.detail.IsVisible())
	}
}
