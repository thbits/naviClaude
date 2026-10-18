package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

// statusCounts tallies live sessions by what they need from the user.
type statusCounts struct {
	working, waiting, done, idle int
}

// countStatuses tallies by effective status, so background work counts as working.
func countStatuses(sessions []*session.Session, bg backgroundMsg) statusCounts {
	var c statusCounts
	for _, s := range sessions {
		switch session.EffectiveStatus(s, bg[s.ID]) {
		case session.StatusActive:
			c.working++
		case session.StatusWaiting:
			c.waiting++
		case session.StatusIdle:
			// Matches the sidebar's "finished while you were away" icon.
			if s.Attention == attention.Done {
				c.done++
			} else {
				c.idle++
			}
		}
	}
	return c
}

// renderStatusSummary renders e.g. "● 2 working · ◎ 1 needs you · ○ 3 idle", skipping
// empty statuses and falling back to bare counts, then nothing, rather than cut a label.
func renderStatusSummary(c statusCounts, budget int) string {
	type part struct {
		n     int
		icon  string
		color lipgloss.Color
		label string
	}
	parts := []part{
		{c.working, styles.IconActive, styles.ColorGreen, "working"},
		{c.waiting, styles.IconWaiting, styles.ColorAmber, "needs you"},
		{c.done, styles.IconDone, styles.ColorBlue, "done"},
		{c.idle, styles.IconIdle, styles.ColorGray, "idle"},
	}
	sep := styles.TitleBarDim.Render(" · ")
	build := func(withLabels bool) string {
		var segs []string
		for _, p := range parts {
			if p.n == 0 {
				continue
			}
			text := fmt.Sprintf(" %d", p.n)
			if withLabels {
				text += " " + p.label
			}
			segs = append(segs, lipgloss.NewStyle().Foreground(p.color).Render(p.icon)+styles.TitleBarDim.Render(text))
		}
		return strings.Join(segs, sep)
	}
	for _, withLabels := range []bool{true, false} {
		if s := build(withLabels); lipgloss.Width(s) <= budget {
			return s
		}
	}
	return ""
}
