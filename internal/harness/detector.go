package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/tmux"
)

// PaneSource is the tmux surface the Detector needs; *tmux.Client satisfies it.
type PaneSource interface {
	ListPanes() ([]tmux.PaneInfo, error)
	CapturePaneOutput(target string) (string, error)
}

// Detector discovers live sessions of every registered harness by inspecting
// the process tree of each tmux pane.
type Detector struct {
	panes        PaneSource
	registry     *Registry
	buildTree    func() *session.ProcessTree
	activeWindow time.Duration // how recently a harness must have written to count as active
	cpuThreshold float64       // subtree CPU% above which a session counts as working
	tracker      *session.StatusTracker
	selfPID      int // naviClaude's own process
	parentPID    int // its launching shell, the pane process of a floating pane
}

// NewDetector creates a Detector. activeWindowSecs <= 0 defaults to 5s and
// cpuThreshold <= 0 defaults to 5%.
func NewDetector(panes PaneSource, reg *Registry, activeWindowSecs int, cpuThreshold float64) *Detector {
	if activeWindowSecs <= 0 {
		activeWindowSecs = 5
	}
	if cpuThreshold <= 0 {
		cpuThreshold = 5.0
	}
	activeWindow := time.Duration(activeWindowSecs) * time.Second
	return &Detector{
		panes:        panes,
		registry:     reg,
		buildTree:    session.BuildProcessTree,
		activeWindow: activeWindow,
		cpuThreshold: cpuThreshold,
		tracker:      session.NewStatusTracker(activeWindow),
		selfPID:      os.Getpid(),
		parentPID:    os.Getppid(),
	}
}

// Detect returns all live sessions found by walking the process tree of every
// tmux pane, using a single bulk ps call.
func (d *Detector) Detect() ([]*session.Session, error) {
	panes, err := d.panes.ListPanes()
	if err != nil {
		return nil, fmt.Errorf("detector: list panes: %w", err)
	}

	tree := d.buildTree()

	var sessions []*session.Session
	for _, pane := range panes {
		// tmux 3.3+ names popup sessions "[popup]".
		if strings.HasPrefix(pane.SessionName, "[popup]") || pane.SessionName == "popup" {
			continue
		}
		// Don't detect naviClaude's own pane (a plain or floating one). The
		// parent check holds even when the ps snapshot missed this process.
		if pane.PID == d.selfPID || pane.PID == d.parentPID || tree.IsAncestorOf(pane.PID, d.selfPID) {
			continue
		}
		h, s := d.sessionFromPane(pane, tree)
		if s == nil {
			continue
		}
		s.Status = d.ResolveStatus(h, s, tree)
		sessions = append(sessions, s)
	}

	// Drop hysteresis entries for targets no longer live so the tracker map
	// doesn't grow unbounded.
	activeTargets := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		if s.TmuxTarget != "" {
			activeTargets[s.TmuxTarget] = true
		}
	}
	d.tracker.Prune(activeTargets)

	return sessions, nil
}

// sessionFromPane returns the first harness whose process runs in the pane
// (directly or as a descendant) and the session built for it.
func (d *Detector) sessionFromPane(pane tmux.PaneInfo, tree *session.ProcessTree) (Harness, *session.Session) {
	for _, h := range d.registry.All() {
		// Check the pane's own process by tmux command and by ps basename: some
		// CLIs (e.g. newer Claude) retitle themselves, so tmux reports the title.
		if h.MatchProcess(pane.CurrentCommand) || h.MatchProcess(tree.Name(pane.PID)) {
			return h, d.buildSession(h, pane, pane.PID, tree)
		}
		if pid := tree.FindMatchingDescendant(pane.PID, h.MatchProcess, 6); pid != 0 {
			return h, d.buildSession(h, pane, pid, tree)
		}
	}
	return nil, nil
}

// buildSession fills the tmux/process fields generically, then lets the
// harness enrich the rest.
func (d *Detector) buildSession(h Harness, pane tmux.PaneInfo, pid int, tree *session.ProcessTree) *session.Session {
	s := &session.Session{
		TmuxSession:  pane.SessionName,
		TmuxTarget:   pane.Target,
		TmuxPaneID:   pane.PaneID,
		Attention:    pane.Attention,
		CWD:          pane.CurrentPath,
		Status:       session.StatusIdle,
		LastActivity: time.Now(),
		ProjectName:  filepath.Base(pane.CurrentPath),
		PID:          pid,
		Harness:      h.Info().Name,
	}
	if parentApp := tree.ParentAppName(pid); parentApp != "" {
		s.Subprocess = true
		s.SubprocessParent = parentApp
	}
	s.CPU, s.Memory = tree.Stats(pid)
	h.Enrich(s)
	return s
}

// ResolveStatus returns the harness's native status when it has one. Otherwise
// it blends pane-content, process-subtree CPU and recent-write signals through
// the tracker's priority (Waiting > Working > Idle) and hysteresis.
func (d *Detector) ResolveStatus(h Harness, s *session.Session, tree *session.ProcessTree) session.SessionStatus {
	if status, ok := h.NativeStatus(s); ok {
		return status
	}
	// Without a native status, a hook-raised prompt is the next best signal.
	if s.Attention == "waiting" {
		return session.StatusWaiting
	}
	now := time.Now()

	// A subprocess pane belongs to the parent app, so its content says nothing
	// about the agent.
	signal := session.SignalNone
	if s.TmuxTarget != "" && !s.Subprocess {
		if content, err := d.panes.CapturePaneOutput(s.TmuxTarget); err == nil {
			signal = h.ClassifyPane(content)
		}
	}
	cpuActive := tree.SubtreeCPU(s.PID) >= d.cpuThreshold
	recent := h.RecentlyActive(s, d.activeWindow)
	return d.tracker.Resolve(s.TmuxTarget, signal, cpuActive, recent, now)
}

// PreviewStatus resolves status for the preview path from already-captured
// content: native status first, else Waiting when a prompt is visible. The
// caller applies only Waiting, so Active is a "not waiting" sentinel.
func PreviewStatus(h Harness, s *session.Session, content string) session.SessionStatus {
	if status, ok := h.NativeStatus(s); ok {
		return status
	}
	if h.ClassifyPane(content) == session.SignalWaiting {
		return session.StatusWaiting
	}
	return session.StatusActive
}
