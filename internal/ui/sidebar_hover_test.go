package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

func hoverTestSessions() []*session.Session {
	return []*session.Session{
		{ID: "one", TmuxSession: "work", TmuxTarget: "work:1.0", ProjectName: "alpha", Status: session.StatusIdle, LastActivity: time.Now()},
		{ID: "two", TmuxSession: "work", TmuxTarget: "work:2.0", ProjectName: "beta", Status: session.StatusIdle, LastActivity: time.Now()},
	}
}

func TestSidebarHoverHighlightsRowAndSurvivesRefresh(t *testing.T) {
	forceColorProfile(t)
	hoverBg := termenv.TrueColor.Color(string(styles.ColorBgHover)).Sequence(true)

	m := NewSidebar(40, 20)
	m.SetSessions(hoverTestSessions())
	m.SetSize(39, 19)
	betaIdx := -1
	for i, it := range m.flatItems {
		if !it.isGroup && it.session.ID == "two" {
			betaIdx = i
		}
	}
	if betaIdx < 0 || betaIdx == m.cursor {
		t.Fatalf("need beta as a non-selected row (idx %d, cursor %d)", betaIdx, m.cursor)
	}
	betaLine := func() string {
		for _, l := range strings.Split(m.View(), "\n") {
			if strings.Contains(l, "beta") {
				return l
			}
		}
		t.Fatal("beta row not rendered")
		return ""
	}

	if strings.Contains(betaLine(), hoverBg) {
		t.Fatal("beta is highlighted before any hover")
	}
	maxWidth := func() int {
		w := 0
		for _, l := range strings.Split(m.View(), "\n") {
			w = max(w, lipgloss.Width(l))
		}
		return w
	}
	before := maxWidth()
	m.SetHover(betaIdx)
	if after := maxWidth(); after != before {
		t.Errorf("hover changed the widest line from %d to %d cells", before, after)
	}
	if !strings.Contains(betaLine(), hoverBg) {
		t.Fatal("hovered row lacks the hover background")
	}

	m.SetSessions(hoverTestSessions()) // periodic refresh: new pointers, same rows
	if !strings.Contains(betaLine(), hoverBg) {
		t.Error("hover was lost on refresh")
	}
	m.SetHover(-1)
	if strings.Contains(betaLine(), hoverBg) {
		t.Error("hover not cleared")
	}
}
