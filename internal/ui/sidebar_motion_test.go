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
	"github.com/thbits/naviClaude/internal/styles"
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

var leadNum = regexp.MustCompile(`^\s*(\d+)\D`)

// sessionNumbers returns the number before each session's status icon in flat
// order, "" for group headers, the selected row and unnumbered sessions.
func sessionNumbers(m *SidebarModel) []string {
	lines := strings.Split(m.content, "\n")
	nums := make([]string, len(m.flatItems))
	for i, l := range lines {
		owner := m.lineItems[i]
		if owner < 0 || m.flatItems[owner].isGroup || (i > 0 && m.lineItems[i-1] == owner) {
			continue
		}
		if g := leadNum.FindStringSubmatch(ansi.Strip(l)); g != nil {
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
	want = map[int]string{1: "3", 3: "1", 4: "", 5: "1", 14: "9", 16: "11"} // the cursor row shows its bar
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
		if !firstLine && after[i] != before[i] {
			t.Errorf("line %d (below a row's title) changed:\n%q\n%q", i, ansi.Strip(before[i]), ansi.Strip(after[i]))
		}
	}
	m.SetLineNumbers("off")
	for i, n := range sessionNumbers(&m) {
		if n != "" {
			t.Fatalf("off should draw no numbers, row %d shows %q", i, n)
		}
	}
}

func TestPendingCountHighlightsMatchingNumbers(t *testing.T) {
	forceColorProfile(t)
	m := motionSidebar(40, 60)
	m.SetLineNumbers("absolute") // cursor on header aa, so every session shows its number
	titleOf := func(row int) string {
		for i, owner := range m.lineItems {
			if owner == row {
				return strings.Split(m.content, "\n")[i]
			}
		}
		t.Fatalf("no title line for row %d", row)
		return ""
	}
	amber := func(s string) string { return lipgloss.NewStyle().Foreground(styles.ColorAmber).Bold(true).Render(s) }
	gray := func(s string) string { return lipgloss.NewStyle().Foreground(styles.ColorGray).Render(s) }
	dim := func(s string) string { return lipgloss.NewStyle().Foreground(styles.ColorDim).Render(s) }

	if l := titleOf(1); !strings.Contains(l, dim("1")) {
		t.Errorf("with no count, numbers should be faint: %q", l)
	}
	m.SetPendingCount("1")
	if l := titleOf(1); !strings.Contains(l, amber("1")) {
		t.Errorf("1 matches the count 1 and should be amber: %q", l)
	}
	if l := titleOf(10); !strings.Contains(l, amber("1")+gray("0")) {
		t.Errorf("10 should light its typed 1 and gray the rest: %q", l)
	}
	if l := titleOf(2); !strings.Contains(l, dim("2")) {
		t.Errorf("2 doesn't start with 1 and should stay faint: %q", l)
	}
	m.SetPendingCount("")
	if l := titleOf(10); !strings.Contains(l, dim("10")) {
		t.Errorf("numbers should go faint once the count is gone: %q", l)
	}
}

func TestCountShowsNumbersWhenOff(t *testing.T) {
	m := motionSidebar(40, 60)
	m.SetCursor(4)
	m.SetPendingCount("3")
	nums := sessionNumbers(&m)
	if nums[3] != "1" || nums[5] != "1" || nums[4] != "" {
		t.Errorf("a pending count with numbers off should show relative numbers, got %v", nums)
	}
	m.SetPendingCount("")
	for i, n := range sessionNumbers(&m) {
		if n != "" {
			t.Fatalf("numbers should hide again once the count is used, row %d shows %q", i, n)
		}
	}
}
