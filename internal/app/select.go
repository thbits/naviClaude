package app

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// selectAttempts is how many session loads a requested selection waits for its session to appear.
const selectAttempts = 6

// handoffAPI is how another process asks this dashboard to select a session.
type handoffAPI interface {
	TakeSelect(pane string) string
}

// selectRequestMsg carries a selection asked for through the pane option.
type selectRequestMsg struct{ target string }

// WithSelect starts the dashboard on target, a tmux pane id or a session id,
// instead of the last focused session.
func (m Model) WithSelect(target string) Model {
	m.requestSelect(target)
	if target != "" {
		m.restoredLastSession = true
	}
	return m
}

func (m *Model) requestSelect(target string) {
	m.pendingSelect, m.selectTries = target, selectAttempts
}

// pollSelectCmd reads, and clears, a selection request left on this pane.
func (m Model) pollSelectCmd() tea.Cmd {
	pane, h := m.selfPane, m.handoff
	if pane == "" || h == nil {
		return nil
	}
	return func() tea.Msg {
		if t := h.TakeSelect(pane); t != "" {
			return selectRequestMsg{target: t}
		}
		return nil
	}
}

// selfPaneID is this process's tmux pane, "" outside tmux.
func selfPaneID() string { return os.Getenv("TMUX_PANE") }

// findSelectTarget returns the session whose pane id or session id is the pending target.
func (m *Model) findSelectTarget() *session.Session {
	var byID *session.Session
	for _, s := range m.sessions {
		if s.TmuxPaneID != "" && s.TmuxPaneID == m.pendingSelect {
			return s
		}
		if byID == nil && s.ID != "" && (s.ID == m.pendingSelect || s.Key() == m.pendingSelect) {
			byID = s
		}
	}
	return byID
}

// typing reports whether keys now go to a text input or a pane, where a moved cursor would misdirect them.
func (m *Model) typing() bool {
	switch m.mode {
	case ModePassthrough, ModeNameInput, ModeRenameSession, ModeDirPicker, ModeResumePicker, ModePreviewSearch:
		return true
	}
	return false
}

// applyPendingSelect selects the requested session once it is listed, opening
// its group and clearing a filter that hides it, then focuses a live one so keys
// go straight to it. After selectAttempts loads without it the request is dropped.
func (m *Model) applyPendingSelect() tea.Cmd {
	if m.pendingSelect == "" {
		return nil
	}
	s := m.findSelectTarget()
	var leave tea.Cmd
	if s != nil {
		leave = m.leaveForJump()
	}
	if s == nil || m.typing() || !m.selectInSidebar(s) {
		if m.selectTries--; m.selectTries <= 0 {
			m.pendingSelect = ""
		}
		return leave
	}
	m.pendingSelect = ""
	return joinCmds(leave, m.enterJumped(s))
}

// leaveForJump drops passthrough and a search, which a jump to another session wins over.
func (m *Model) leaveForJump() tea.Cmd {
	var leave tea.Cmd
	if m.mode == ModePassthrough {
		leave = m.setFocus(ModeList)
	}
	if m.search.IsActive() {
		m.endSearch()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
	}
	return leave
}

// enterJumped syncs the panes to the just-selected s and focuses it when live.
func (m *Model) enterJumped(s *session.Session) tea.Cmd {
	cmd := m.afterSelectionChange()
	if s.Status != session.StatusClosed && s.TmuxPaneID != "" {
		cmd = joinCmds(cmd, m.setFocus(ModePassthrough))
	}
	return cmd
}

func (m *Model) selectInSidebar(s *session.Session) bool {
	group := s.TmuxSession
	if s.Status == session.StatusClosed {
		group = ui.ClosedGroupName
	}
	pick := func() bool {
		m.sidebar.ExpandGroup(group)
		if s.Key() != "" {
			return m.sidebar.SelectByID(s.Key())
		}
		return m.sidebar.SelectByTarget(s.TmuxTarget)
	}
	if pick() {
		return true
	}
	if m.sidebar.StatusFilter() == ui.FilterNone {
		return false
	}
	m.sidebar.SetStatusFilter(ui.FilterNone)
	return pick()
}

// joinCmds batches the non-nil commands, or returns nil when there are none.
func joinCmds(cmds ...tea.Cmd) tea.Cmd {
	var out []tea.Cmd
	for _, c := range cmds {
		if c != nil {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return tea.Batch(out...)
}
