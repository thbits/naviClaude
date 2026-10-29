package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/session"
)

func groupRows(m *SidebarModel) string {
	var out []string
	for _, it := range m.flatItems {
		if it.isGroup {
			out = append(out, "["+it.groupName+"]")
		} else {
			out = append(out, it.session.ID)
		}
	}
	return strings.Join(out, " ")
}

func TestManualOrderFollowsWindowsAndDraggedGroups(t *testing.T) {
	now := time.Now()
	m := NewSidebar(40, 40)
	m.SetSessionSortOrder("manual")
	m.SetGroupSortOrder("manual")
	m.SetGroupOrder([]string{"zz", "aa"})
	m.SetSessions([]*session.Session{
		{ID: "a10", TmuxSession: "aa", TmuxTarget: "aa:10.0", Status: session.StatusIdle, LastActivity: now},
		{ID: "a2", TmuxSession: "aa", TmuxTarget: "aa:2.0", Status: session.StatusIdle, LastActivity: now.Add(-time.Hour)},
		{ID: "z1", TmuxSession: "zz", TmuxTarget: "zz:1.0", Status: session.StatusIdle, LastActivity: now},
		{ID: "n1", TmuxSession: "new", TmuxTarget: "new:1.0", Status: session.StatusIdle, LastActivity: now},
	})
	if got, want := groupRows(&m), "[zz] z1 [aa] a2 a10 [new] n1"; got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
}

func TestManualOrderFloatsWaitingAndUnseenDone(t *testing.T) {
	now := time.Now()
	m := NewSidebar(40, 40)
	m.SetSessionSortOrder("manual")
	m.SetGroupSortOrder("manual")
	m.SetGroupOrder([]string{"aa", "bb"})
	m.SetSessions([]*session.Session{
		{ID: "a1", TmuxSession: "aa", TmuxTarget: "aa:1.0", Status: session.StatusIdle, LastActivity: now},
		{ID: "b1", TmuxSession: "bb", TmuxTarget: "bb:1.0", Status: session.StatusIdle, LastActivity: now},
		{ID: "b2", TmuxSession: "bb", TmuxTarget: "bb:2.0", Status: session.StatusWaiting, LastActivity: now},
		{ID: "b3", TmuxSession: "bb", TmuxTarget: "bb:3.0", Status: session.StatusIdle, Attention: attention.Done, LastActivity: now},
	})
	if got, want := groupRows(&m), "[bb] b2 b3 b1 [aa] a1"; got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
}

func TestMovedGroupOrderKeepsAbsentGroups(t *testing.T) {
	m := NewSidebar(40, 40)
	m.SetGroupSortOrder("manual")
	m.SetGroupOrder([]string{"gone", "aa", "bb", "cc"})
	now := time.Now()
	m.SetSessions([]*session.Session{
		{ID: "a", TmuxSession: "aa", TmuxTarget: "aa:1.0", LastActivity: now},
		{ID: "b", TmuxSession: "bb", TmuxTarget: "bb:1.0", LastActivity: now},
		{ID: "c", TmuxSession: "cc", TmuxTarget: "cc:1.0", LastActivity: now},
	})
	got := strings.Join(m.MovedGroupOrder("cc", GroupDrop{Anchor: "aa", Before: true}), ",")
	if want := "cc,aa,bb,gone"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
}

func sectionSidebar(order string, pinned []string, ss ...*session.Session) *SidebarModel {
	m := NewSidebar(40, 60)
	m.SetSize(40, 60)
	m.SetSessionSortOrder(order)
	m.SetGroupSortOrder("manual")
	m.SetPinned(pinned)
	m.SetSessions(ss)
	return &m
}

func row(id, group string, win int, st session.SessionStatus) *session.Session {
	return &session.Session{ID: id, TmuxSession: group, TmuxTarget: group + ":" + strconv.Itoa(win) + ".0",
		TmuxPaneID: "%" + id, Status: st, LastActivity: time.Now()}
}

func TestSectionsOrderPinnedWaitingDoneWorkingIdle(t *testing.T) {
	done := row("d", "g", 2, session.StatusIdle)
	done.Attention = attention.Done
	m := sectionSidebar("manual", []string{"p"},
		row("i", "g", 1, session.StatusIdle), done, row("w", "g", 3, session.StatusActive),
		row("q", "g", 4, session.StatusWaiting), row("p", "g", 5, session.StatusActive))
	if got, want := groupRows(m), "[g] p q d w i"; got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
	view := ansi.Strip(m.content)
	for _, label := range []string{"─ pinned ─", "─ waiting ─", "─ done ─", "─ working ─", "─ idle ─"} {
		if !strings.Contains(view, label) {
			t.Errorf("no %q line in:\n%s", label, view)
		}
	}
}

func TestOneSectionDrawsNoLine(t *testing.T) {
	m := sectionSidebar("manual", nil, row("a", "g", 1, session.StatusIdle), row("b", "g", 2, session.StatusIdle))
	if strings.Contains(ansi.Strip(m.content), "─ idle") {
		t.Errorf("section line drawn for a single section:\n%s", ansi.Strip(m.content))
	}
}

func TestRowThatStartsWorkingJumpsToWorkingSection(t *testing.T) {
	a, b := row("a", "g", 1, session.StatusIdle), row("b", "g", 2, session.StatusIdle)
	m := sectionSidebar("manual", nil, a, b)
	b.Status = session.StatusActive
	m.SetSessions([]*session.Session{a, b})
	if got, want := groupRows(m), "[g] b a"; got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
}

func TestPinnedGroupFloatsAboveWaitingGroup(t *testing.T) {
	m := sectionSidebar("manual", []string{"p"},
		row("q", "aa", 1, session.StatusWaiting), row("p", "bb", 1, session.StatusIdle), row("x", "cc", 1, session.StatusIdle))
	if got, want := groupRows(m), "[bb] p [aa] q [cc] x"; got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
}

func TestTogglePinMovesRowAndBack(t *testing.T) {
	a, b := row("a", "g", 1, session.StatusIdle), row("b", "g", 2, session.StatusIdle)
	m := sectionSidebar("manual", nil, a, b)
	if !m.TogglePin(b) || groupRows(m) != "[g] b a" {
		t.Fatalf("after pin: rows = %s", groupRows(m))
	}
	if m.TogglePin(b) || groupRows(m) != "[g] a b" {
		t.Errorf("after unpin: rows = %s", groupRows(m))
	}
}

func TestDropStaysWithinSectionInsideAGroup(t *testing.T) {
	w, i := row("w", "g", 1, session.StatusActive), row("i", "g", 2, session.StatusIdle)
	other := row("o", "h", 1, session.StatusActive)
	m := sectionSidebar("manual", nil, w, i, other)
	if m.sameSection(i, w) {
		t.Error("an idle row may land among working rows of its own group")
	}
	if !m.sameSection(i, other) {
		t.Error("a drop into another tmux session was refused")
	}
}

func TestPinBeforeIDMovesToIDOnceKnown(t *testing.T) {
	a, b := row("a", "g", 1, session.StatusIdle), row("", "g", 2, session.StatusIdle)
	b.TmuxPaneID = "%9"
	m := sectionSidebar("manual", nil, a, b)
	if !m.TogglePin(b) || m.Pinned()[0] != "%9" {
		t.Fatalf("pins = %v, want [%%9]", m.Pinned())
	}
	b.ID = "real"
	m.SetSessions([]*session.Session{a, b})
	if got := m.Pinned(); len(got) != 1 || got[0] != "real" {
		t.Errorf("pins = %v, want [real]", got)
	}
	if groupRows(m) != "[g] real a" {
		t.Errorf("rows = %s, want the pinned row first", groupRows(m))
	}
}

func TestTopSectionLabeledWhenGroupHasSeveral(t *testing.T) {
	m := sectionSidebar("manual", nil, row("w", "g", 1, session.StatusActive), row("i", "g", 2, session.StatusIdle))
	view := ansi.Strip(m.content)
	if !strings.Contains(view, "─ working ─") || !strings.Contains(view, "─ idle ─") {
		t.Errorf("want working and idle lines:\n%s", view)
	}
	if strings.Index(view, "─ working ─") > strings.Index(view, "w") && strings.Index(view, "─ working ─") > strings.Index(view, "○") {
		t.Errorf("working line is not above the working row:\n%s", view)
	}
}

func TestPinnedGroupLeadsTheList(t *testing.T) {
	m := sectionSidebar("manual", []string{"p"},
		row("q", "aa", 1, session.StatusWaiting), row("p", "bb", 1, session.StatusIdle), row("x", "cc", 1, session.StatusIdle))
	if !m.ToggleGroupPin("cc") {
		t.Fatal("cc did not pin")
	}
	if got, want := groupRows(m), "[cc] x [bb] p [aa] q"; got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
	if m.ToggleGroupPin(ClosedGroupName) {
		t.Error("the Closed group pinned")
	}
	m.RenameGroup("cc", "dd")
	if !m.IsGroupPinned("dd") || m.IsGroupPinned("cc") {
		t.Errorf("pin did not follow the rename: %v", m.PinnedGroups())
	}
}
