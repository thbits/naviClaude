package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/tmux"
)

// Statuses reported by the scripting commands.
const (
	stWaiting = "waiting"
	stWorking = "working"
	stDone    = "done"
	stIdle    = "idle"
)

// agentInfo is one live session as list/wait/send report it; JSON names are a stable interface.
type agentInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`     // why it is waiting, "" otherwise
	Background   string `json:"background"` // background work keeping it working, e.g. "1 agent"
	TmuxSession  string `json:"tmux_session"`
	Target       string `json:"target"`
	PaneID       string `json:"pane_id"`
	CWD          string `json:"cwd"`
	Project      string `json:"project"`
	Branch       string `json:"branch"`
	Model        string `json:"model"`
	LastActivity string `json:"last_activity"` // RFC3339, "" when unknown
	PID          int    `json:"pid"`
	Self         bool   `json:"self"` // the pane this command runs in
	Subprocess   bool   `json:"subprocess"`

	stamp int64 // changes whenever Claude's status or transcript moves; turn-start detection only
}

// statusLabel is the status with its reason, e.g. "waiting (needs approval: Edit)".
func (a agentInfo) statusLabel() string {
	if a.Reason != "" {
		return a.Status + " (" + a.Reason + ")"
	}
	if a.Background != "" && a.Status == stWorking {
		return a.Status + " (" + a.Background + ")"
	}
	return a.Status
}

// agentSource returns the current live sessions; tests swap in fakes.
type agentSource func() ([]agentInfo, error)

// liveSource discovers sessions with the TUI's own detector, kept across calls so
// status hysteresis and transcript caches carry over between polls.
type liveSource struct {
	reg      *harness.Registry
	detector *harness.Detector
	aliases  *session.AliasStore
	selfPane string
	branches bool // look up git branches (list only; wait polls skip the git calls)
}

func newLiveSource(branches bool) *liveSource {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude: config:", err)
	}
	tc := tmux.New()
	reg := harness.NewRegistry(claude.New(claude.Options{
		Command:      cfg.ClaudeCommand,
		ProcessNames: cfg.ProcessNames,
	}))
	d := harness.NewDetector(tc, reg, cfg.ActiveWindowSecs, cfg.CPUActiveThreshold)
	d.IncludeOwnPane()
	return &liveSource{
		reg:      reg,
		detector: d,
		aliases:  session.NewAliasStore(""),
		selfPane: os.Getenv("TMUX_PANE"),
		branches: branches,
	}
}

// Agents runs one detection pass and converts each session.
func (l *liveSource) Agents() ([]agentInfo, error) {
	sessions, err := l.detector.Detect()
	if err != nil {
		return nil, err
	}
	aliases := l.aliases.All()
	out := make([]agentInfo, 0, len(sessions))
	for _, s := range sessions {
		if name, ok := aliases[s.Key()]; ok {
			s.DisplayName = name
		}
		var act *session.SessionActivity
		if h, ok := l.reg.For(s); ok && s.ID != "" {
			if ap, ok := harness.AsActivity(h); ok {
				act, _ = ap.LoadActivity(s)
			}
		}
		a := toAgentInfo(s, act)
		a.Self = l.selfPane != "" && s.TmuxPaneID == l.selfPane
		if l.branches {
			if ref, ok := session.CurrentBranch(s.CWD); ok {
				a.Branch = ref.Branch
			}
		}
		var updated int64
		if st, ok := claude.ReadNativeState(s.PID, s.ID); ok {
			updated = st.UpdatedAt
		}
		a.stamp = turnStamp(updated, s.SessionFile)
		out = append(out, a)
	}
	sortAgents(out)
	return out, nil
}

// turnStamp combines Claude's status-change time with the transcript's mtime; a
// missing transcript adds nothing, since LastActivity then defaults to now.
func turnStamp(statusUpdatedAt int64, transcript string) int64 {
	stamp := statusUpdatedAt
	if transcript == "" {
		return stamp
	}
	if info, err := os.Stat(transcript); err == nil {
		stamp ^= info.ModTime().UnixNano()
	}
	return stamp
}

// toAgentInfo maps a detected session and its activity (may be nil) to the reported shape.
func toAgentInfo(s *session.Session, act *session.SessionActivity) agentInfo {
	a := agentInfo{
		ID:          s.ID,
		Name:        sessionName(s),
		TmuxSession: s.TmuxSession,
		Target:      s.TmuxTarget,
		PaneID:      s.TmuxPaneID,
		CWD:         s.CWD,
		Project:     s.ProjectName,
		Branch:      s.GitBranch,
		Model:       s.Model,
		PID:         s.PID,
		Subprocess:  s.Subprocess,
	}
	if act != nil && act.Model != "" {
		a.Model = act.Model
	}
	if !s.LastActivity.IsZero() {
		a.LastActivity = s.LastActivity.UTC().Format(time.RFC3339)
	}
	a.Status, a.Reason = deriveStatus(s, act)
	if bg := act.Background(); a.Status == stWorking && bg.Busy() {
		a.Background = describeBackground(bg)
	}
	return a
}

// sessionName mirrors the sidebar: rename/title > slug > project.
func sessionName(s *session.Session) string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	if s.Slug != "" {
		return s.Slug
	}
	return s.ProjectName
}

// deriveStatus mirrors the TUI: background work makes idle working, and an idle
// pane the Stop hook marked unseen is done.
func deriveStatus(s *session.Session, act *session.SessionActivity) (status, reason string) {
	switch session.EffectiveStatus(s, act.Background()) {
	case session.StatusWaiting:
		return stWaiting, waitingReason(s, act)
	case session.StatusActive:
		return stWorking, ""
	}
	if s.Attention == attention.Done {
		return stDone, ""
	}
	return stIdle, ""
}

// waitingReason follows the sidebar detail's wording (internal/ui/sidebar_detail.go).
func waitingReason(s *session.Session, act *session.SessionActivity) string {
	tool, target := "", ""
	if act != nil {
		tool, target = act.Tool, act.Target
	}
	switch tool {
	case "AskUserQuestion":
		if target != "" {
			return "question: " + target
		}
		return "question"
	case "ExitPlanMode":
		return "plan approval"
	case "":
		if s.WaitingFor != "" {
			return s.WaitingFor
		}
		return "needs input"
	}
	if target != "" {
		return "needs approval: " + tool + " " + target
	}
	return "needs approval: " + tool
}

// describeBackground renders running background work, e.g. "2 agents, 1 shell".
func describeBackground(bg session.Background) string {
	var parts []string
	add := func(n int, one string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %ss", n, one))
		}
	}
	add(bg.Agents, "agent")
	add(bg.Shells, "shell")
	add(bg.Monitors, "monitor")
	add(bg.Workflows, "workflow")
	return strings.Join(parts, ", ")
}

// sortAgents orders by tmux session, then window and pane number.
func sortAgents(list []agentInfo) {
	sort.SliceStable(list, func(i, j int) bool {
		si, wi, pi, oki := session.ParseTarget(list[i].Target)
		sj, wj, pj, okj := session.ParseTarget(list[j].Target)
		if !oki || !okj {
			return list[i].Target < list[j].Target
		}
		if si != sj {
			return si < sj
		}
		if wi != wj {
			return wi < wj
		}
		return pi < pj
	})
}

// errNoMatch and errAmbiguous classify resolveTarget failures.
var (
	errNoMatch   = errors.New("no live session matches")
	errAmbiguous = errors.New("ambiguous target")
)

// resolveTarget finds the one session q names: exact ID, pane id, tmux target or
// name, then a unique ID prefix of 6+ hex chars. Each tier must be unique to win.
func resolveTarget(list []agentInfo, q string) (agentInfo, error) {
	if q == "" {
		return agentInfo{}, fmt.Errorf("%w: empty target", errNoMatch)
	}
	tiers := []func(a agentInfo) bool{
		func(a agentInfo) bool { return a.ID == q },
		func(a agentInfo) bool { return a.PaneID == q },
		func(a agentInfo) bool { return a.Target == q },
		func(a agentInfo) bool { return a.Name == q },
		func(a agentInfo) bool { return strings.EqualFold(a.Name, q) },
		func(a agentInfo) bool { return idPrefixOK(q) && strings.HasPrefix(a.ID, q) },
	}
	for _, match := range tiers {
		var hits []agentInfo
		for _, a := range list {
			if match(a) {
				hits = append(hits, a)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], nil
		default:
			var b strings.Builder
			fmt.Fprintf(&b, "%v %q matches %d sessions:", errAmbiguous, q, len(hits))
			for _, h := range hits {
				fmt.Fprintf(&b, "\n  %s  %s  %s  %s", h.PaneID, h.Target, h.Name, h.ID)
			}
			b.WriteString("\nuse the pane id or session id instead")
			return agentInfo{}, errors.New(b.String())
		}
	}
	return agentInfo{}, fmt.Errorf("%w %q (see: naviclaude list)", errNoMatch, q)
}

// minIDPrefix keeps a short word (e.g. "a" or "dead") from silently picking a session.
const minIDPrefix = 6

// idPrefixOK reports whether q can stand for a session ID: long enough and only hex or dashes.
func idPrefixOK(q string) bool {
	if len(q) < minIDPrefix {
		return false
	}
	for _, r := range q {
		if r != '-' && !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// parseInterleaved parses flags wherever they appear among positionals, so
// "wait <target> --until idle" works; everything after "--" is positional.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if used := len(args) - len(rest); used > 0 && args[used-1] == "--" {
			return append(pos, rest...), nil
		}
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// agoText is a compact age like "45s", "12m", "3h" or "2d".
func agoText(rfc string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(0, int(d.Seconds())))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
