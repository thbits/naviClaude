package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/session"
)

// motionSidebar holds header aa, 12 sessions, header bb, 3 sessions: 17 rows.
func motionSidebar(w, h int) SidebarModel {
	now := time.Now()
	var sessions []*session.Session
	for i := 1; i <= 15; i++ {
		group := "aa"
		if i > 12 {
			group = "bb"
		}
		sessions = append(sessions, &session.Session{
			ID: fmt.Sprintf("s%02d", i), ProjectName: fmt.Sprintf("p%02d", i), TmuxSession: group,
			TmuxTarget: fmt.Sprintf("%s:%d.0", group, i), Status: session.StatusIdle,
			LastActivity: now.Add(-time.Duration(i) * time.Second),
		})
	}
	m := NewSidebar(w, h)
	m.SetSessions(sessions)
	m.SetSize(w, h)
	m.SetCursor(0)
	return m
}

func TestGoToRowClamps(t *testing.T) {
	m := motionSidebar(40, 40)
	m.GoToRow(4)
	if m.Cursor() != 3 {
		t.Errorf("GoToRow(4) cursor = %d, want 3", m.Cursor())
	}
	m.GoToRow(500)
	if m.Cursor() != len(m.flatItems)-1 {
		t.Errorf("GoToRow(500) cursor = %d, want the last row", m.Cursor())
	}
}

func TestGoToSessionSkipsHeaders(t *testing.T) {
	m := motionSidebar(40, 40)
	m.GoToSession(13) // aa holds sessions 1-12, so 13 sits under header bb
	if m.Cursor() != 14 {
		t.Errorf("GoToSession(13) cursor = %d, want 14", m.Cursor())
	}
	m.GoToSession(500)
	if m.Cursor() != len(m.flatItems)-1 {
		t.Errorf("GoToSession(500) cursor = %d, want the last session", m.Cursor())
	}
}

func TestMoveSessionsSkipsHeaders(t *testing.T) {
	m := motionSidebar(40, 40)
	m.SetCursor(12) // session 12, the last in aa
	m.MoveSessions(1)
	if m.Cursor() != 14 {
		t.Errorf("1 session down from row 12 = %d, want 14 (past header bb)", m.Cursor())
	}
	m.MoveSessions(-2)
	if m.Cursor() != 11 {
		t.Errorf("2 sessions up from row 14 = %d, want 11", m.Cursor())
	}
	m.MoveSessions(-50)
	if m.Cursor() != 1 {
		t.Errorf("past the top = %d, want the first session, not the header", m.Cursor())
	}
}

var summaryNum = regexp.MustCompile(`^[\s▎]*(\d+) `)

// sessionNumbers returns the number on each row's summary line in flat order,
// "" for group headers and unnumbered sessions.
func sessionNumbers(m *SidebarModel) []string {
	lines := strings.Split(m.content, "\n")
	nums := make([]string, len(m.flatItems))
	for i, l := range lines {
		owner := m.lineItems[i]
		// The summary is a session's second line.
		if owner < 0 || m.flatItems[owner].isGroup || i == 0 || m.lineItems[i-1] != owner || (i > 1 && m.lineItems[i-2] == owner) {
			continue
		}
		if g := summaryNum.FindStringSubmatch(ansi.Strip(l)); g != nil {
			nums[owner] = g[1]
		}
	}
	return nums
}

func TestLineNumbersCountSessionsOnly(t *testing.T) {
	m := motionSidebar(40, 60)
	m.SetLineNumbers("absolute")
	nums := sessionNumbers(&m)
	want := map[int]string{0: "", 1: "1", 12: "12", 13: "", 14: "13", 16: "15"}
	for i, w := range want {
		if nums[i] != w {
			t.Errorf("absolute row %d = %q, want %q (all: %v)", i, nums[i], w, nums)
		}
	}

	m.SetLineNumbers("relative")
	m.SetCursor(4)
	nums = sessionNumbers(&m)
	want = map[int]string{1: "3", 3: "1", 4: "4", 5: "1", 14: "9", 16: "11"} // the cursor row shows its own number
	for i, w := range want {
		if nums[i] != w {
			t.Errorf("relative row %d = %q, want %q (all: %v)", i, nums[i], w, nums)
		}
	}

	m.SetCursor(13) // on header bb: the sessions either side are both 1 away
	nums = sessionNumbers(&m)
	if nums[12] != "1" || nums[14] != "1" || nums[13] != "" {
		t.Errorf("around a header cursor = %q, %q, %q; want 1, \"\", 1", nums[12], nums[13], nums[14])
	}
}

func TestLineNumbersLeaveRowsInPlace(t *testing.T) {
	m := motionSidebar(40, 60)
	m.SetCursor(4)
	before := strings.Split(m.content, "\n")
	m.SetLineNumbers("relative")
	after := strings.Split(m.content, "\n")
	if len(after) != len(before) {
		t.Fatalf("%d lines with numbers, %d without", len(after), len(before))
	}
	for i := range before {
		if lipgloss.Width(after[i]) != lipgloss.Width(before[i]) {
			t.Errorf("line %d width %d with numbers, %d without", i, lipgloss.Width(after[i]), lipgloss.Width(before[i]))
		}
		owner := m.lineItems[i]
		firstLine := owner >= 0 && (i == 0 || m.lineItems[i-1] != owner)
		if firstLine && after[i] != before[i] {
			t.Errorf("line %d (a row's first line) changed:\n%q\n%q", i, ansi.Strip(before[i]), ansi.Strip(after[i]))
		}
	}
	m.SetLineNumbers("off")
	for i, n := range sessionNumbers(&m) {
		if n != "" {
			t.Fatalf("off should draw no numbers, row %d shows %q", i, n)
		}
	}
}
