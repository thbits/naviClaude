package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

// segment is a run of detail-line text in one color.
type segment struct {
	text string
	fg   lipgloss.Color
	flex bool // shrinks first when the line is too wide, so the tail stays visible
}

func seg(text string, fg lipgloss.Color) segment     { return segment{text: text, fg: fg} }
func flexSeg(text string, fg lipgloss.Color) segment { return segment{text: text, fg: fg, flex: true} }

// renderSegments styles segs on bg and cuts at maxWidth so a line never wraps;
// each segment sets the background since an inner ANSI reset drops the parent's.
func renderSegments(segs []segment, bg lipgloss.Color, maxWidth int) string {
	total := 0
	for _, s := range segs {
		total += ansi.StringWidth(s.text)
	}
	if over := total - maxWidth; over > 0 {
		segs = append([]segment(nil), segs...)
		for i, s := range segs {
			if !s.flex {
				continue
			}
			// Keep at least a few cells of the flexible text.
			keep := ansi.StringWidth(s.text) - over
			if keep < 4 {
				keep = 4
			}
			segs[i].text = truncateDisplay(s.text, keep)
			break
		}
	}
	var b strings.Builder
	left := maxWidth
	for _, s := range segs {
		if left <= 0 {
			break
		}
		text := s.text
		if w := ansi.StringWidth(text); w > left {
			text = truncateDisplay(text, left)
			left = 0
		} else {
			left -= w
		}
		b.WriteString(lipgloss.NewStyle().Foreground(s.fg).Background(bg).Render(text))
	}
	return b.String()
}

// activitySegments describes what the session is doing now: the tool in
// flight, what it waits on the user for, or when it finished.
func activitySegments(s *session.Session, a *session.SessionActivity, now time.Time) []segment {
	gray := styles.ColorGray
	if s.Status == session.StatusClosed {
		return []segment{seg("closed "+agoText(s.LastActivity, now), styles.ColorDimText)}
	}
	if s.Restoring {
		return []segment{seg(styles.IconRestore+" ", styles.ColorCyan), seg("restoring", gray)}
	}

	var segs []segment
	switch {
	case s.Status == session.StatusWaiting || (s.Status == session.StatusActive && a != nil && waitsOnUser(a.Tool)):
		segs = waitingSegments(s, a)
	case s.Status == session.StatusActive:
		// No status icon: the row's breathing dot already shows it is working.
		if a == nil {
			segs = []segment{seg("working", styles.ColorFg)}
			break
		}
		bg := session.Background{Agents: a.Agents, Shells: a.Shells}
		if a.Tool == "" && a.TurnEnded && bg.Busy() {
			// The main thread is done; the pane stays busy only for background work.
			return append(backgroundSegments(bg), seg(" running", styles.ColorGray))
		}
		if (a.Tool == "Agent" || a.Tool == "Task") && bg.Agents == 1 {
			bg.Agents = 0 // the one agent is already the tool shown
		}
		// Counts lead since the tool and elapsed time change from tick to tick.
		if bgSegs := backgroundSegments(bg); len(bgSegs) > 0 {
			segs = append(bgSegs, seg(" · ", gray))
		}
		if a.Tool != "" {
			segs = append(segs, seg(a.Tool, styles.ColorFg))
			if a.Target != "" {
				segs = append(segs, flexSeg(" · "+a.Target, gray))
			}
		} else {
			segs = append(segs, seg(styles.IconThinking+" ", styles.ColorGreen), seg("thinking", styles.ColorFg))
		}
		if !a.TurnStart.IsZero() {
			elapsed := elapsedText(now.Sub(a.TurnStart))
			if a.TurnStartApprox {
				elapsed += "+"
			}
			segs = append(segs, seg(" · "+elapsed, gray))
		}
		return segs
	case s.Summary == "" && (a == nil || a.LastAssistant.IsZero()):
		segs = []segment{seg(styles.IconIdle+" ", gray), seg("new session", gray)}
	default:
		done := s.LastActivity
		if a != nil && !a.LastAssistant.IsZero() {
			done = a.LastAssistant
		}
		segs = []segment{seg("✓ ", styles.ColorBlue), seg("done "+agoText(done, now), gray)}
	}

	if a != nil {
		bg := session.Background{Agents: a.Agents, Shells: a.Shells}
		if bgSegs := backgroundSegments(bg); len(bgSegs) > 0 {
			segs = append(append(segs, seg(" · ", gray)), bgSegs...)
		}
	}
	return segs
}

// backgroundSegments renders "󰚩 2 agents ·  1 shell · 󰧑 thinking", leaving out
// a zero count.
func backgroundSegments(bg session.Background) []segment {
	var segs []segment
	if bg.Agents > 0 {
		segs = append(segs, seg(fmt.Sprintf("%s %d %s", styles.IconAgent, bg.Agents, plural(bg.Agents, "agent")), styles.ColorPurple))
	}
	if bg.Shells > 0 {
		if len(segs) > 0 {
			segs = append(segs, seg(" · ", styles.ColorGray))
		}
		segs = append(segs, seg(fmt.Sprintf("%s %d %s", styles.IconShell, bg.Shells, plural(bg.Shells, "shell")), styles.ColorCyan))
	}
	if bg.Thinking && len(segs) > 0 {
		segs = append(segs, seg(" · ", styles.ColorGray), seg(styles.IconThinking+" ", styles.ColorGreen), seg("thinking", styles.ColorGray))
	}
	return segs
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// waitsOnUser reports tools that block on the user by design, whatever the
// pane heuristics currently say.
func waitsOnUser(tool string) bool { return tool == "AskUserQuestion" || tool == "ExitPlanMode" }

func waitingSegments(s *session.Session, a *session.SessionActivity) []segment {
	icon := seg("⏸ ", styles.ColorAmber)
	tool, target := "", ""
	if a != nil {
		tool, target = a.Tool, a.Target
	}
	switch tool {
	case "AskUserQuestion":
		segs := []segment{icon, seg("question", styles.ColorAmber)}
		if target != "" {
			segs = append(segs, flexSeg(": "+target, styles.ColorFg))
		}
		return segs
	case "ExitPlanMode":
		return []segment{icon, seg("plan approval", styles.ColorAmber)}
	case "":
		reason := s.WaitingFor
		if reason == "" {
			reason = "needs input"
		}
		return []segment{icon, flexSeg(reason, styles.ColorAmber)}
	}
	segs := []segment{icon, seg("needs approval: ", styles.ColorAmber), seg(tool, styles.ColorFg)}
	if target != "" {
		segs = append(segs, flexSeg(" "+target, styles.ColorGray))
	}
	return segs
}

// agoText is "just now" under a minute, else e.g. "5m ago".
func agoText(t, now time.Time) string {
	if t.IsZero() {
		return "a while ago"
	}
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	return relativeTime(d) + " ago"
}

// elapsedText formats a running duration with second precision under a
// minute, since that is when it is changing fastest.
func elapsedText(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		if mins := int(d.Minutes()) % 60; mins > 0 {
			return fmt.Sprintf("%dh%dm", h, mins)
		}
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Context-fill thresholds: amber warns ahead of auto-compact, red is close to it.
const (
	ctxWarnPct = 0.60
	ctxHighPct = 0.85
)

// ctxBarColor colors the context bar by how full the window is.
func ctxBarColor(pct float64) lipgloss.Color {
	switch {
	case pct > ctxHighPct:
		return styles.ColorRed
	case pct >= ctxWarnPct:
		return styles.ColorAmber
	default:
		return styles.ColorCyan
	}
}

// formatTokens renders a token count compactly: 950, 62K, 1M, 1.5M.
func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	case n >= 1000:
		return fmt.Sprintf("%dK", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// ctxLine renders "ctx ━━━──── 62K/1M (6%)" within maxWidth, or "" when
// there is no usage to show.
func ctxLine(used, limit int, bg lipgloss.Color, maxWidth int) string {
	if limit <= 0 || used <= 0 {
		return ""
	}
	pct := float64(used) / float64(limit)
	if pct > 1 {
		pct = 1
	}
	label := fmt.Sprintf(" %s/%s (%d%%)", formatTokens(used), formatTokens(limit), int(pct*100))
	barWidth := maxWidth - len("ctx ") - ansi.StringWidth(label)
	if barWidth > 20 {
		barWidth = 20
	}
	if barWidth < 3 {
		barWidth = 3
	}
	filled := int(pct * float64(barWidth))
	return renderSegments([]segment{
		seg("ctx ", styles.ColorCyan),
		seg(strings.Repeat("━", filled), ctxBarColor(pct)),
		seg(strings.Repeat("─", barWidth-filled), styles.ColorBorder),
		seg(label, styles.ColorGray),
	}, bg, maxWidth)
}

// repoSegments renders " naviClaude ⎇ main · 3 files +120/−8" within maxWidth;
// the file part appears only once the changed-files list has been loaded. The
// repo name shrinks first and is dropped when the branch would be cut instead.
func repoSegments(ref session.GitRef, files []session.ChangedFile, maxWidth int) []segment {
	segs := branchSegments(ref.Branch, files)
	if ref.Repo == "" {
		return segs
	}
	repo := []segment{seg(styles.IconRepo+" ", styles.ColorPurple), flexSeg(ref.Repo, styles.ColorFg), seg(" ", styles.ColorFg)}
	over := segmentsWidth(repo) + segmentsWidth(segs) - maxWidth
	if over > ansi.StringWidth(ref.Repo)-4 {
		return segs
	}
	return append(repo, segs...)
}

func segmentsWidth(segs []segment) int {
	w := 0
	for _, s := range segs {
		w += ansi.StringWidth(s.text)
	}
	return w
}

func branchSegments(branch string, files []session.ChangedFile) []segment {
	segs := []segment{seg("⎇ ", styles.ColorPurple), flexSeg(branch, styles.ColorGreen)}
	if len(files) == 0 {
		return segs
	}
	added, removed := 0, 0
	for _, f := range files {
		added += f.Added
		removed += f.Removed
	}
	noun := "files"
	if len(files) == 1 {
		noun = "file"
	}
	return append(segs,
		seg(fmt.Sprintf(" · %d %s ", len(files), noun), styles.ColorGray),
		seg(fmt.Sprintf("+%d", added), styles.ColorGreen),
		seg("/", styles.ColorGray),
		seg(fmt.Sprintf("−%d", removed), styles.ColorRed),
	)
}
