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

var leadingNum = regexp.MustCompile(`^\s*(\d+) `)

// gutterNumbers returns the number shown on each row's first line, in order.
func gutterNumbers(m *SidebarModel) []string {
	var nums []string
	for i, l := range strings.Split(m.content, "\n") {
		owner := m.lineItems[i]
		if owner < 0 || (i > 0 && m.lineItems[i-1] == owner) {
			continue
		}
		if g := leadingNum.FindStringSubmatch(ansi.Strip(l)); g != nil {
			nums = append(nums, g[1])
		} else {
			nums = append(nums, "")
		}
	}
	return nums
}

func TestLineNumbersAbsoluteAndRelative(t *testing.T) {
	m := motionSidebar(40, 60)
	m.SetLineNumbers("absolute")
	nums := gutterNumbers(&m)
	if len(nums) != 17 || nums[0] != "1" || nums[16] != "17" {
		t.Fatalf("absolute numbers = %v, want 1..17", nums)
	}

	m.SetLineNumbers("relative")
	m.SetCursor(4)
	nums = gutterNumbers(&m)
	want := map[int]string{0: "4", 3: "1", 4: "5", 5: "1", 16: "12"} // the cursor row shows its own row
	for i, w := range want {
		if nums[i] != w {
			t.Errorf("relative row %d = %q, want %q (all: %v)", i, nums[i], w, nums)
		}
	}
}

func TestLineNumbersKeepPaneWidth(t *testing.T) {
	m := motionSidebar(40, 60)
	widest := func() int {
		w := 0
		for _, l := range strings.Split(m.content, "\n") {
			w = max(w, lipgloss.Width(l))
		}
		return w
	}
	before := widest()
	m.SetLineNumbers("relative")
	if after := widest(); after != before {
		t.Errorf("widest line %d with numbers, %d without; the gutter must come out of the rows", after, before)
	}
	m.SetLineNumbers("off")
	if gutterNumbers(&m)[0] != "" {
		t.Error("off should draw no numbers")
	}
}

func TestLineNumbersDroppedWhenTooNarrow(t *testing.T) {
	m := motionSidebar(minRowWidth+1, 60)
	m.SetLineNumbers("absolute")
	if m.gutterWidth() != 0 {
		t.Errorf("gutter = %d at width %d, want none", m.gutterWidth(), m.width)
	}
}
