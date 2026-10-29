package ui

import (
	"strings"
	"testing"
	"time"

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
