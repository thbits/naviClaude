package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
)

// historyModel is clickModel with pane ids on its live sessions, so they can be focused.
func historyModel(t *testing.T) *Model {
	t.Helper()
	m, _ := clickModel(t)
	for _, s := range m.sessions {
		if s.Status != session.StatusClosed {
			s.TmuxPaneID = "%9" + s.ID
		}
	}
	return m
}

// focusRow selects the session with id and enters it, as Enter does.
func focusRow(t *testing.T, m *Model, id string) {
	t.Helper()
	m.setFocus(ModeList)
	if !m.sidebar.SelectByID(id) {
		t.Fatalf("no row for %q", id)
	}
	m.afterSelectionChange()
	next, _ := m.focusSelected()
	*m = next.(Model)
	if m.mode != ModePassthrough {
		t.Fatalf("precondition: focusing %q left mode %v", id, m.mode)
	}
}

func back(m *Model) tea.Cmd    { return press(m, tea.MouseButtonBackward, xList, yOne) }
func forward(m *Model) tea.Cmd { return press(m, tea.MouseButtonForward, xList, yOne) }

func wantAt(t *testing.T, m *Model, id string, mode Mode) {
	t.Helper()
	if got := selectedID(m); got != id || m.mode != mode {
		t.Fatalf("selected %q in %v, want %q in %v", got, m.mode, id, mode)
	}
}

func TestMouseBackForwardWithoutHistoryDoesNothing(t *testing.T) {
	m := historyModel(t)
	m.sidebar.SelectByID("a2")
	back(m)
	wantAt(t, m, "a2", ModeList)
	forward(m)
	wantAt(t, m, "a2", ModeList)
}

func TestMouseBackForwardWalkFocusHistory(t *testing.T) {
	m := historyModel(t)
	focusRow(t, m, "a1")
	focusRow(t, m, "a2")

	back(m)
	wantAt(t, m, "a1", ModePassthrough)
	back(m) // nothing older
	wantAt(t, m, "a1", ModePassthrough)
	forward(m)
	wantAt(t, m, "a2", ModePassthrough)
	forward(m) // nothing newer
	wantAt(t, m, "a2", ModePassthrough)
}

// Back returns to the last focused session first when the cursor browsed away from it.
func TestMouseBackAfterBrowsingReturnsToLastFocused(t *testing.T) {
	m := historyModel(t)
	focusRow(t, m, "a1")
	focusRow(t, m, "a2")
	press(m, tea.MouseButtonWheelUp, xList, yOne) // select a1 without focusing it
	m.afterSelectionChange()
	wantAt(t, m, "a1", ModeList)

	back(m)
	wantAt(t, m, "a2", ModePassthrough)
	back(m)
	wantAt(t, m, "a1", ModePassthrough)
}

// Focusing another session after Back keeps every earlier focus.
func TestMouseFocusAfterBackKeepsHistory(t *testing.T) {
	m := deepHistoryModel(t)
	for _, id := range []string{"a1", "a2", "a3"} {
		focusRow(t, m, id)
	}
	back(m)
	back(m)
	wantAt(t, m, "a1", ModePassthrough)
	focusRow(t, m, "a4")
	for _, id := range []string{"a3", "a2", "a1"} {
		back(m)
		wantAt(t, m, id, ModePassthrough)
	}
}

// Refocusing an older session moves it to the newest end, keeping one entry each.
func TestMouseRefocusMovesSessionToNewest(t *testing.T) {
	m := deepHistoryModel(t)
	for _, id := range []string{"a1", "a2", "a3", "a1"} {
		focusRow(t, m, id)
	}
	for _, id := range []string{"a3", "a2"} {
		back(m)
		wantAt(t, m, id, ModePassthrough)
	}
	back(m) // a1 is no longer listed before a2
	wantAt(t, m, "a2", ModePassthrough)
}

// Refocusing the current entry (Enter again after Back) keeps the forward history.
func TestMouseRefocusKeepsForwardHistory(t *testing.T) {
	m := historyModel(t)
	focusRow(t, m, "a1")
	focusRow(t, m, "a2")
	back(m)
	focusRow(t, m, "a1")
	forward(m)
	wantAt(t, m, "a2", ModePassthrough)
}

func TestMouseBackSkipsSessionsNoLongerListed(t *testing.T) {
	m := historyModel(t)
	focusRow(t, m, "a1")
	m.recordFocus(&session.Session{ID: "gone", TmuxPaneID: "%99"})
	focusRow(t, m, "a2")

	back(m)
	wantAt(t, m, "a1", ModePassthrough)
	forward(m)
	wantAt(t, m, "a2", ModePassthrough)
}

// A session that has since closed is selected in the Closed group, without passthrough.
func TestMouseBackToClosedSessionSelectsIt(t *testing.T) {
	m := historyModel(t)
	m.recordFocus(m.sessions[2]) // c1, now closed
	focusRow(t, m, "a2")

	back(m)
	wantAt(t, m, "c1", ModeList)
	forward(m)
	wantAt(t, m, "a2", ModePassthrough)
}

// A click on a row counts as focusing it, though it only selects the session.
func TestMouseBackAfterRowClicks(t *testing.T) {
	m := historyModel(t)
	press(m, tea.MouseButtonLeft, xList, yOne)
	release(m, xList, yOne)
	press(m, tea.MouseButtonLeft, xList, yTwo)
	release(m, xList, yTwo)
	wantAt(t, m, "a2", ModeList)

	back(m)
	wantAt(t, m, "a1", ModePassthrough)
	forward(m)
	wantAt(t, m, "a2", ModePassthrough)
}

// deepHistoryModel is clickModel listing four live sessions, a1 to a4.
func deepHistoryModel(t *testing.T) *Model {
	t.Helper()
	m, _ := clickModel(t)
	var ss []*session.Session
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		ss = append(ss, &session.Session{ID: id, DisplayName: id, TmuxSession: "zzmouse", TmuxTarget: "zzmouse:0." + id[1:], TmuxPaneID: "%9" + id, Status: session.StatusIdle, LastActivity: m.clock()})
	}
	m.sessions, m.allSessions = ss, ss
	m.sidebar.SetSessions(ss)
	return m
}

func TestMouseBackWalksDeepHistory(t *testing.T) {
	m := deepHistoryModel(t)
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		focusRow(t, m, id)
	}
	for _, id := range []string{"a3", "a2", "a1"} {
		back(m)
		wantAt(t, m, id, ModePassthrough)
	}
	for _, id := range []string{"a2", "a3", "a4"} {
		forward(m)
		wantAt(t, m, id, ModePassthrough)
	}
}

func TestMouseBackOverStatusBar(t *testing.T) {
	m := historyModel(t)
	focusRow(t, m, "a1")
	focusRow(t, m, "a2")
	press(m, tea.MouseButtonBackward, xList, m.height-1)
	wantAt(t, m, "a1", ModePassthrough)
}

func TestMouseBackInertUnderPromptsAndOverlays(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(m *Model)
	}{
		{"kill prompt", func(m *Model) { m.confirmKill = true }},
		{"help overlay", func(m *Model) { m.help.Toggle() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := historyModel(t)
			focusRow(t, m, "a1")
			focusRow(t, m, "a2")
			tc.set(m)
			back(m)
			if got := selectedID(m); got != "a2" {
				t.Fatalf("selected %q, want a2 kept", got)
			}
		})
	}
}

func TestMouseBackLeavesSearch(t *testing.T) {
	m := historyModel(t)
	focusRow(t, m, "a1")
	focusRow(t, m, "a2")
	m.setFocus(ModeList)
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(m.keys.Search)})
	*m = next.(Model)
	if m.mode != ModeSearch {
		t.Fatalf("precondition: mode %v, want search", m.mode)
	}
	back(m)
	wantAt(t, m, "a1", ModePassthrough)
	if m.search.IsActive() {
		t.Fatal("search still active after Back")
	}
}
