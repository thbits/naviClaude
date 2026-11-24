package app

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
)

// focusHistoryCap bounds how many focused sessions Back can walk through.
const focusHistoryCap = 50

// focusEntry names a focused session by its key, or by pane for a placeholder without one.
type focusEntry struct{ key, pane string }

func (e focusEntry) matches(s *session.Session) bool {
	if s == nil {
		return false
	}
	if e.key != "" {
		return s.Key() == e.key
	}
	return s.TmuxPaneID == e.pane
}

// recordFocus moves s to the newest end of the focus history, which keeps each
// session once in most-recently-focused order. Refocusing the entry Back or
// Forward reached changes nothing, so stepping can carry on from it.
func (m *Model) recordFocus(s *session.Session) {
	if s == nil || (s.Key() == "" && s.TmuxPaneID == "") {
		return
	}
	h := m.focusHistory
	if m.focusPos < len(h) && h[m.focusPos].matches(s) {
		return
	}
	h = slices.DeleteFunc(slices.Clone(h), func(e focusEntry) bool { return e.matches(s) })
	h = append(h, focusEntry{key: s.Key(), pane: s.TmuxPaneID})
	if len(h) > focusHistoryCap {
		h = h[len(h)-focusHistoryCap:]
	}
	m.focusHistory, m.focusPos = h, len(h)-1
}

// listedSession returns the listed session e names, or nil once it is gone.
func (m *Model) listedSession(e focusEntry) *session.Session {
	for _, s := range m.sessions {
		if e.matches(s) {
			return s
		}
	}
	return nil
}

// stepFocusHistory jumps to the previous (dir < 0) or next focused session,
// skipping ones no longer listed.
func (m Model) stepFocusHistory(dir int) (tea.Model, tea.Cmd) {
	h := m.focusHistory
	if len(h) == 0 {
		return m, nil
	}
	cur := m.sidebar.SelectedSession()
	i := m.focusPos + dir
	// Browsed away from the current entry: Back returns to it first.
	if dir < 0 && !h[m.focusPos].matches(cur) {
		i = m.focusPos
	}
	for ; i >= 0 && i < len(h); i += dir {
		s := m.listedSession(h[i])
		if s == nil || h[i].matches(cur) {
			continue
		}
		leave := m.leaveForJump()
		if m.mode == ModePreviewSearch {
			leave = joinCmds(leave, m.setFocus(ModeList))
		}
		if !m.selectInSidebar(s) {
			return m, leave
		}
		m.focusPos = i // before focusing, so the jump does not record itself
		return m, joinCmds(leave, m.enterJumped(s))
	}
	return m, nil
}
