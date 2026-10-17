package app

import (
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
)

// sidebarDetailMsg carries the selected session's live activity and the git
// branch of its working directory.
type sidebarDetailMsg struct {
	sessionID string
	activity  *session.SessionActivity
	cwd       string
	ref       session.GitRef
	inRepo    bool
}

// detailInflight holds the session IDs with a detail load running, so a read
// stuck on a hung mount is never joined by another one every tick.
var detailInflight = struct {
	sync.Mutex
	ids map[string]bool
}{ids: map[string]bool{}}

// beginDetailLoad claims id, reporting false when a load for it is running.
func beginDetailLoad(id string) bool {
	detailInflight.Lock()
	defer detailInflight.Unlock()
	if detailInflight.ids[id] {
		return false
	}
	detailInflight.ids[id] = true
	return true
}

func endDetailLoad(id string) {
	detailInflight.Lock()
	delete(detailInflight.ids, id)
	detailInflight.Unlock()
}

// loadSidebarDetailCmd reads the transcript tail and git branch off the UI
// goroutine. Both are cached underneath, so polling every tick is cheap.
func loadSidebarDetailCmd(h harness.Harness, sess *session.Session) tea.Cmd {
	// The goroutine reads a copy: the sidebar may replace or mutate sess.
	snap := *sess
	return func() tea.Msg {
		// Claimed here rather than when the Cmd is built, so a Cmd that is
		// never run cannot leave the session stuck as in flight.
		if !beginDetailLoad(snap.ID) {
			return nil
		}
		defer endDetailLoad(snap.ID)
		msg := sidebarDetailMsg{sessionID: snap.ID, cwd: snap.CWD}
		if ap, ok := harness.AsActivity(h); ok && snap.Status != session.StatusClosed {
			msg.activity, _ = ap.LoadActivity(&snap)
		}
		msg.ref, msg.inRepo = session.CurrentBranch(snap.CWD)
		return msg
	}
}

// sidebarDetailCmd refreshes the detail lines of the selected session.
func (m *Model) sidebarDetailCmd() tea.Cmd {
	sel := m.sidebar.SelectedSession()
	if sel == nil || sel.ID == "" {
		return nil
	}
	return loadSidebarDetailCmd(m.harnessFor(sel), sel)
}

func (m *Model) applySidebarDetail(msg sidebarDetailMsg) {
	m.sidebar.SetBranch(msg.cwd, msg.ref, msg.inRepo)
	if sel := m.sidebar.SelectedSession(); sel != nil && sel.ID == msg.sessionID {
		m.sidebar.SetActivity(msg.sessionID, msg.activity)
	}
}

// backgroundMsg carries the running subagents and shells of every live session.
type backgroundMsg map[string]session.Background

// backgroundInflight keeps one background-count scan running at a time.
var backgroundInflight atomic.Bool

// backgroundCmd counts running subagents and shells for every live session, so
// each sidebar row can show them. Transcript tails are cached, so this is mostly stats.
func (m *Model) backgroundCmd() tea.Cmd {
	type job struct {
		h    harness.ActivityProvider
		snap session.Session
	}
	var jobs []job
	for _, s := range m.sessions {
		if s.ID == "" || s.Status == session.StatusClosed {
			continue
		}
		if ap, ok := harness.AsActivity(m.harnessFor(s)); ok {
			jobs = append(jobs, job{ap, *s})
		}
	}
	return func() tea.Msg {
		if !backgroundInflight.CompareAndSwap(false, true) {
			return nil
		}
		defer backgroundInflight.Store(false)
		counts := backgroundMsg{}
		for _, j := range jobs {
			if a, _ := j.h.LoadActivity(&j.snap); a != nil && (a.Agents > 0 || a.Shells > 0) {
				counts[j.snap.ID] = session.Background{Agents: a.Agents, Shells: a.Shells}
			}
		}
		return counts
	}
}
