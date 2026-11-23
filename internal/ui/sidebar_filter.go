package ui

import (
	"slices"

	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/session"
)

// StatusFilter narrows the session list to the sessions in one status.
type StatusFilter int

const (
	FilterNone StatusFilter = iota
	FilterWaiting
	FilterDone
	FilterWorking
	FilterIdle
)

// String is the filter's label, as the title bar shows it.
func (f StatusFilter) String() string {
	switch f {
	case FilterWaiting:
		return "waiting"
	case FilterDone:
		return "done"
	case FilterWorking:
		return "working"
	case FilterIdle:
		return "idle"
	}
	return ""
}

// statusOf classifies a live session as the title bar counts it, whatever section
// it sorts into: a pinned session that waits on the user is still waiting.
func (m *SidebarModel) statusOf(s *session.Session) StatusFilter {
	if s == nil || s.Restoring {
		return FilterNone
	}
	switch session.EffectiveStatus(s, m.background[s.ID]) {
	case session.StatusWaiting:
		return FilterWaiting
	case session.StatusActive:
		return FilterWorking
	case session.StatusIdle:
		if s.Attention == attention.Done {
			return FilterDone
		}
		return FilterIdle
	}
	return FilterNone
}

// StatusFilter returns the active status filter.
func (m *SidebarModel) StatusFilter() StatusFilter { return m.statusFilter }

// filtering reports whether the filter narrows the list now; search shows every
// match, so the filter waits until it ends.
func (m *SidebarModel) filtering() bool {
	return m.statusFilter != FilterNone && !m.searchActive
}

// groupCollapsed reports whether a group's rows are hidden; a filter shows every
// match, collapsed group or not.
func (m *SidebarModel) groupCollapsed(name string) bool {
	return m.collapsed[name] && !m.filtering()
}

// shown reports whether the filter keeps s. The cursor's row stays once it stops
// matching: selecting a done row marks it seen, and the next row would follow.
func (m *SidebarModel) shown(s *session.Session) bool {
	if m.statusOf(s) == m.statusFilter {
		return true
	}
	return m.stickyKey != "" && workingKey(s) == m.stickyKey
}

// SetStatusFilter shows only the sessions in status f (FilterNone: all of them),
// keeping the selected session selected while it is still listed.
func (m *SidebarModel) SetStatusFilter(f StatusFilter) {
	if f == m.statusFilter {
		return
	}
	sel := m.SelectedSession()
	header := m.SelectedGroupName()
	m.statusFilter = f
	m.stickyKey = ""
	if sel != nil && f == FilterNone && sel.Status != session.StatusClosed && m.collapsed[sel.TmuxSession] {
		// A filter shows rows of collapsed groups; keep that row visible.
		m.collapsed[sel.TmuxSession] = false
		m.userToggled[sel.TmuxSession] = true
	}
	m.rebuildFlatItems()
	m.cursor = 0
	found := false
	for i, it := range m.flatItems {
		if (sel != nil && it.session == sel) || (sel == nil && it.isGroup && it.groupName == header) {
			m.cursor, found = i, true
			break
		}
	}
	m.clampCursor()
	if !found {
		m.snapToNearestSession()
	}
	m.updateTracked()
	m.syncViewport()
}

// attentionRing lists what the next-attention key cycles through, in display
// order: sessions waiting on the user, then done ones, else the working ones.
func (m *SidebarModel) attentionRing() []*session.Session {
	var waiting, done, working []*session.Session
	for _, g := range m.groups {
		if g.Name == ClosedGroupName {
			continue
		}
		for _, s := range g.Sessions {
			st := m.statusOf(s)
			if m.filtering() && st != m.statusFilter {
				continue
			}
			switch st {
			case FilterWaiting:
				waiting = append(waiting, s)
			case FilterDone:
				done = append(done, s)
			case FilterWorking:
				working = append(working, s)
			}
		}
	}
	if ring := append(waiting, done...); len(ring) > 0 {
		return ring
	}
	return working
}

// SelectNextAttention moves to the ring member after the selection (the first when
// the selection is outside the ring), expanding its group; ring is the ring's size.
func (m *SidebarModel) SelectNextAttention() (moved bool, ring int) {
	members := m.attentionRing()
	if len(members) == 0 {
		return false, 0
	}
	next := members[0]
	if i := slices.Index(members, m.SelectedSession()); i >= 0 {
		if len(members) == 1 {
			return false, 1
		}
		next = members[(i+1)%len(members)]
	}
	if m.groupCollapsed(next.TmuxSession) {
		m.collapsed[next.TmuxSession] = false
		m.userToggled[next.TmuxSession] = true
		m.rebuildFlatItems()
	}
	for i, it := range m.flatItems {
		if it.session == next {
			m.cursor = i
			m.updateTracked()
			m.syncViewport()
			return true, len(members)
		}
	}
	return false, len(members)
}
