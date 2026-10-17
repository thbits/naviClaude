package app

import (
	"sync"

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
	branch    string
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
		msg.branch, msg.inRepo = session.CurrentBranch(snap.CWD)
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
	m.sidebar.SetBranch(msg.cwd, msg.branch, msg.inRepo)
	if sel := m.sidebar.SelectedSession(); sel != nil && sel.ID == msg.sessionID {
		m.sidebar.SetActivity(msg.sessionID, msg.activity)
	}
}
