package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/session"
)

// statusSessions is two groups covering every status, plus a closed session.
func statusSessions() []*session.Session {
	now := time.Now()
	return []*session.Session{
		{ID: "a-wait", TmuxSession: "aa", TmuxTarget: "aa:1.0", TmuxPaneID: "%1", Status: session.StatusWaiting, LastActivity: now},
		{ID: "a-done", TmuxSession: "aa", TmuxTarget: "aa:2.0", TmuxPaneID: "%2", Status: session.StatusIdle, Attention: attention.Done, LastActivity: now},
		{ID: "a-work", TmuxSession: "aa", TmuxTarget: "aa:3.0", TmuxPaneID: "%3", Status: session.StatusActive, LastActivity: now},
		{ID: "a-idle", TmuxSession: "aa", TmuxTarget: "aa:4.0", TmuxPaneID: "%4", Status: session.StatusIdle, LastActivity: now},
		{ID: "b-done", TmuxSession: "bb", TmuxTarget: "bb:1.0", TmuxPaneID: "%5", Status: session.StatusIdle, Attention: attention.Done, LastActivity: now},
		{ID: "b-idle", TmuxSession: "bb", TmuxTarget: "bb:2.0", TmuxPaneID: "%6", Status: session.StatusIdle, LastActivity: now},
		{ID: "c-idle", TmuxSession: "cc", TmuxTarget: "cc:1.0", TmuxPaneID: "%7", Status: session.StatusIdle, LastActivity: now},
		{ID: "closed", Status: session.StatusClosed, LastActivity: now.Add(-time.Hour)},
	}
}

func filterSidebar() *SidebarModel {
	m := NewSidebar(40, 40)
	m.userToggled[ClosedGroupName] = true // open, so the filter has something to hide
	m.SetSessions(statusSessions())
	return &m
}

func selectID(t *testing.T, m *SidebarModel, id string) {
	t.Helper()
	if !m.SelectByID(id) {
		t.Fatalf("session %q is not listed: %s", id, groupRows(m))
	}
}

func selectedID(m *SidebarModel) string {
	if s := m.SelectedSession(); s != nil {
		return s.ID
	}
	return ""
}

func TestStatusFilterShowsOnlyMatchingRowsAndGroups(t *testing.T) {
	for _, tc := range []struct {
		filter StatusFilter
		want   string
	}{
		{FilterNone, "[aa] a-wait a-done a-work a-idle [bb] b-done b-idle [cc] c-idle [Closed] closed"},
		{FilterWaiting, "[aa] a-wait"},
		{FilterDone, "[aa] a-done [bb] b-done"},
		{FilterWorking, "[aa] a-work"},
		{FilterIdle, "[aa] a-idle [bb] b-idle [cc] c-idle"},
	} {
		m := filterSidebar()
		m.SetStatusFilter(tc.filter)
		if got := groupRows(m); got != tc.want {
			t.Errorf("filter %q: rows = %s, want %s", tc.filter, got, tc.want)
		}
	}
}

// Background work makes an idle session working, and the filter must agree
// with the title bar's count of it.
func TestStatusFilterCountsBackgroundWorkAsWorking(t *testing.T) {
	m := filterSidebar()
	m.SetBackground(map[string]session.Background{"b-idle": {Agents: 1}})
	m.SetStatusFilter(FilterWorking)
	if got, want := groupRows(m), "[aa] a-work [bb] b-idle"; got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
}

// A pinned row sorts into the pinned section but is still filtered by status.
func TestStatusFilterMatchesPinnedRowsByStatus(t *testing.T) {
	m := filterSidebar()
	m.TogglePin(m.Sessions[0]) // a-wait
	m.SetStatusFilter(FilterWaiting)
	if got, want := groupRows(m), "[aa] a-wait"; got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
}

func TestStatusFilterHeaderCountsMatchesAndDropsSectionLines(t *testing.T) {
	m := filterSidebar()
	m.SetStatusFilter(FilterDone)
	view := ansi.Strip(m.View())
	for _, label := range sectionLabels {
		if strings.Contains(view, "─ "+label+" ─") {
			t.Errorf("filtered list still draws the %q section line:\n%s", label, view)
		}
	}
	for _, it := range m.flatItems {
		if it.isGroup && it.groupCount != 1 {
			t.Errorf("header %q counts %d sessions, want the 1 that matches", it.groupName, it.groupCount)
		}
	}
	if lines := strings.Split(view, "\n"); len(lines) < 2 || strings.TrimSpace(lines[1]) == "" {
		t.Errorf("first shown header has a spacer above it:\n%s", view)
	}
}

// A filter shows matches inside collapsed groups, and leaves the fold alone.
func TestStatusFilterShowsRowsOfCollapsedGroups(t *testing.T) {
	m := filterSidebar()
	m.collapsed["bb"] = true
	m.userToggled["bb"] = true
	m.rebuildFlatItems()
	m.SetStatusFilter(FilterDone)
	if got, want := groupRows(m), "[aa] a-done [bb] b-done"; got != want {
		t.Fatalf("rows = %s, want %s", got, want)
	}
	m.SetCursor(2) // [bb]
	m.ToggleGroupAtCursor()
	if got, want := groupRows(m), "[aa] a-done [bb] b-done"; got != want {
		t.Errorf("toggling a header under a filter changed the rows: %s", got)
	}
	m.SetStatusFilter(FilterNone)
	if !m.collapsed["bb"] {
		t.Error("clearing the filter from a header lost the group's collapsed state")
	}
}

func TestStatusFilterKeepsSelectionWhenItMatchesAndWhenCleared(t *testing.T) {
	m := filterSidebar()
	selectID(t, m, "b-done")
	m.SetStatusFilter(FilterDone)
	if got := selectedID(m); got != "b-done" {
		t.Fatalf("selected = %q after filtering, want b-done", got)
	}
	m.SetStatusFilter(FilterNone)
	if got := selectedID(m); got != "b-done" {
		t.Errorf("selected = %q after clearing, want b-done", got)
	}
}

func TestStatusFilterMovesSelectionOffHiddenRow(t *testing.T) {
	m := filterSidebar()
	selectID(t, m, "c-idle")
	m.SetStatusFilter(FilterWaiting)
	if got := selectedID(m); got != "a-wait" {
		t.Fatalf("selected = %q, want the first match a-wait", got)
	}
	// Clearing keeps the row the cursor is on now, not the one it left.
	m.SetStatusFilter(FilterNone)
	if got := selectedID(m); got != "a-wait" {
		t.Errorf("selected = %q after clearing, want a-wait", got)
	}
}

// Clearing a filter on a row inside a collapsed group opens the group, or the
// selection would vanish with the filter.
func TestStatusFilterClearExpandsSelectedRowsGroup(t *testing.T) {
	m := filterSidebar()
	m.collapsed["bb"] = true
	m.rebuildFlatItems()
	m.SetStatusFilter(FilterDone)
	selectID(t, m, "b-done")
	m.SetStatusFilter(FilterNone)
	if got := selectedID(m); got != "b-done" {
		t.Errorf("selected = %q after clearing, want b-done", got)
	}
}

// Selecting a done row marks it seen, so it turns idle. It must stay listed
// while the cursor is on it, or each neighbour would be selected and cleared in turn.
func TestStatusFilterKeepsSelectedRowThatStoppedMatching(t *testing.T) {
	m := filterSidebar()
	m.SetStatusFilter(FilterDone)
	selectID(t, m, "a-done")

	seen := statusSessions()
	seen[1].Attention = "" // a-done was seen
	m.SetSessions(seen)
	if got, want := groupRows(m), "[aa] a-done [bb] b-done"; got != want {
		t.Fatalf("rows = %s, want %s", got, want)
	}
	if got := selectedID(m); got != "a-done" {
		t.Fatalf("selected = %q, want a-done", got)
	}

	// Once the cursor moves on, the next refresh drops the row.
	selectID(t, m, "b-done")
	m.SetSessions(seen)
	if got, want := groupRows(m), "[bb] b-done"; got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
	if got := selectedID(m); got != "b-done" {
		t.Errorf("selected = %q, want b-done", got)
	}
}

// When the selected row is gone for good the cursor follows a neighbour, and a
// later return of the old row must not pull it back.
func TestStatusFilterReanchorsWhenSelectedRowDisappears(t *testing.T) {
	m := filterSidebar()
	m.SetStatusFilter(FilterDone)
	selectID(t, m, "a-done")

	gone := statusSessions()
	gone = append(gone[:1], gone[2:]...) // a-done's pane closed
	m.SetSessions(gone)
	if got := selectedID(m); got != "b-done" {
		t.Fatalf("selected = %q, want b-done", got)
	}
	m.SetSessions(statusSessions())
	if got := selectedID(m); got != "b-done" {
		t.Errorf("selected = %q after the old row returned, want b-done", got)
	}
}

func TestStatusFilterEmptyStateNamesTheFilter(t *testing.T) {
	m := NewSidebar(40, 20)
	m.SetSessions(statusSessions()[5:]) // idle and closed only
	m.SetStatusFilter(FilterWaiting)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "No waiting sessions") || !strings.Contains(view, "Esc") {
		t.Errorf("empty filtered list should name the filter and how to leave it:\n%s", view)
	}
	if strings.Contains(view, "Press n to create one") {
		t.Errorf("empty filtered list shows the no-sessions hint:\n%s", view)
	}
}

// Search lists every match; the filter comes back when it ends.
func TestStatusFilterWaitsOutSearch(t *testing.T) {
	m := filterSidebar()
	m.SetStatusFilter(FilterWaiting)
	m.ExpandAll()
	m.SetSessions(statusSessions())
	if got := groupRows(m); !strings.Contains(got, "c-idle") || !strings.Contains(got, "closed") {
		t.Fatalf("search rows = %s, want every session", got)
	}
	// Rows the filter hid above the selection must not shift it.
	if got := selectedID(m); got != "a-wait" {
		t.Errorf("selected = %q once search listed everything, want a-wait", got)
	}
	m.RestoreCollapsed()
	if got, want := groupRows(m), "[aa] a-wait"; got != want {
		t.Errorf("rows after search = %s, want %s", got, want)
	}
}

func ringIDs(m *SidebarModel) string {
	var ids []string
	for _, s := range m.attentionRing() {
		ids = append(ids, s.ID)
	}
	return strings.Join(ids, " ")
}

func TestAttentionRingIsWaitingThenDoneElseWorking(t *testing.T) {
	m := filterSidebar()
	if got, want := ringIDs(m), "a-wait a-done b-done"; got != want {
		t.Errorf("ring = %q, want %q", got, want)
	}

	calm := statusSessions()
	calm[0].Status = session.StatusActive
	calm[1].Attention = ""
	calm[4].Attention = ""
	m.SetSessions(calm)
	if got, want := ringIDs(m), "a-wait a-work"; got != want {
		t.Errorf("ring with nothing waiting or done = %q, want the working sessions %q", got, want)
	}
}

func TestSelectNextAttentionWrapsAndStartsAtFirst(t *testing.T) {
	m := filterSidebar()
	selectID(t, m, "c-idle") // outside the ring
	var got []string
	for range 4 {
		moved, ring := m.SelectNextAttention()
		if !moved || ring != 3 {
			t.Fatalf("SelectNextAttention = %v, %d; want a move within a ring of 3", moved, ring)
		}
		got = append(got, selectedID(m))
	}
	if want := "a-wait a-done b-done a-wait"; strings.Join(got, " ") != want {
		t.Errorf("visited %v, want %s", got, want)
	}
}

func TestSelectNextAttentionExpandsCollapsedGroup(t *testing.T) {
	m := filterSidebar()
	m.collapsed["aa"] = true
	m.collapsed["bb"] = true
	m.rebuildFlatItems()
	m.SetCursor(0)
	if moved, _ := m.SelectNextAttention(); !moved || selectedID(m) != "a-wait" {
		t.Fatalf("selected = %q, want a-wait in the expanded group", selectedID(m))
	}
	if m.collapsed["aa"] || !m.collapsed["bb"] {
		t.Errorf("collapsed = aa:%v bb:%v, want only the target's group opened", m.collapsed["aa"], m.collapsed["bb"])
	}
	// The group stays open across refreshes, as one the user opened.
	m.SetSessions(statusSessions())
	if selectedID(m) != "a-wait" {
		t.Errorf("selected = %q after a refresh, want a-wait", selectedID(m))
	}
}

func TestSelectNextAttentionIncludesPinnedRows(t *testing.T) {
	m := filterSidebar()
	m.TogglePin(m.Sessions[0]) // a-wait moves to the pinned section
	if got, want := ringIDs(m), "a-wait a-done b-done"; got != want {
		t.Errorf("ring = %q, want %q", got, want)
	}
}

func TestSelectNextAttentionStaysInsideFilter(t *testing.T) {
	m := filterSidebar()
	m.SetStatusFilter(FilterDone)
	if got, want := ringIDs(m), "a-done b-done"; got != want {
		t.Errorf("ring under the done filter = %q, want %q", got, want)
	}
	m.SetStatusFilter(FilterWorking)
	if got, want := ringIDs(m), "a-work"; got != want {
		t.Errorf("ring under the working filter = %q, want %q", got, want)
	}
	m.SetStatusFilter(FilterIdle)
	if moved, ring := m.SelectNextAttention(); moved || ring != 0 {
		t.Errorf("SelectNextAttention under the idle filter = %v, %d; want nothing to go to", moved, ring)
	}
}

func TestSelectNextAttentionWithNothingElseToGoTo(t *testing.T) {
	m := NewSidebar(40, 20)
	m.SetSessions(statusSessions()[5:])
	if moved, ring := m.SelectNextAttention(); moved || ring != 0 {
		t.Errorf("empty ring: got %v, %d", moved, ring)
	}
	m.SetSessions(statusSessions()[:1])
	selectID(t, &m, "a-wait")
	if moved, ring := m.SelectNextAttention(); moved || ring != 1 {
		t.Errorf("already on the only member: got %v, %d; want false, 1", moved, ring)
	}
}
