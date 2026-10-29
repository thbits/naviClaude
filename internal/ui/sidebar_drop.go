package ui

import (
	"slices"
	"strings"

	"github.com/thbits/naviClaude/internal/session"
)

// dropLineOwner marks the drag-and-drop insertion line in lineItems.
const dropLineOwner = -2

// SessionDrop is where a dragged session row lands.
type SessionDrop struct {
	Group  string           // tmux session the row moves into
	Anchor *session.Session // row it lands next to; nil appends to Group
	Before bool             // land before Anchor rather than after it
}

// GroupDrop is where a dragged group header lands.
type GroupDrop struct {
	Anchor string // group it lands next to
	Before bool
}

// dropMark is the marker drawn while dragging, plus the drop it stands for.
type dropMark struct {
	key    string // flat item key the marker sits next to
	after  bool   // insertion line below the item rather than above it
	header bool   // highlight the header instead of drawing a line
	sess   SessionDrop
	group  GroupDrop
}

// DragSessionOver marks where the session in pane src would land if dropped at
// sidebar-local (x, y) and returns that drop; ok is false where it can't land.
func (m *SidebarModel) DragSessionOver(x, y int, src string) (SessionDrop, bool) {
	if d, ok := m.onDropLine(x, y); ok {
		return d.sess, true
	}
	hit := m.HitTest(x, y)
	if _, inList := m.lineAt(x, y); inList && hit.Kind == HitNone && m.drop != nil {
		return m.drop.sess, true // a spacer or section line: keep the last spot
	}
	var mark *dropMark
	switch hit.Kind {
	case HitGroup:
		if hit.Group != ClosedGroupName {
			mark = &dropMark{key: m.flatItems[hit.Index].key(), header: true, sess: SessionDrop{Group: hit.Group}}
		}
	case HitSession:
		mark = m.sessionDropMark(hit, src)
	}
	m.setDrop(mark)
	if mark == nil {
		return SessionDrop{}, false
	}
	return mark.sess, true
}

// sessionDropMark places a dragged row before or after the hit row, resolving
// "after a row" to "before the next row" so the line has one place per gap.
func (m *SidebarModel) sessionDropMark(hit SidebarHit, src string) *dropMark {
	srcSess := m.paneSession(src)
	if !movable(hit.Session) || sameWindow(hit.Session, srcSess) {
		return nil
	}
	idx, before := hit.Index, hit.Upper
	if !before {
		if next := idx + 1; next < len(m.flatItems) && !m.flatItems[next].isGroup && true {
			idx, before = next, true
		}
	}
	anchor := m.flatItems[idx].session
	if anchor.TmuxPaneID == src {
		return nil // dropping right above itself
	}
	if before && idx > 0 && !m.flatItems[idx-1].isGroup && m.flatItems[idx-1].session.TmuxPaneID == src {
		return nil // dropping right below itself
	}
	return &dropMark{
		key:   m.flatItems[idx].key(),
		after: !before,
		sess:  SessionDrop{Group: hit.Group, Anchor: anchor, Before: before},
	}
}

// DragGroupOver marks where group src would land if dropped at sidebar-local
// (x, y): it takes the slot of the group under the point.
func (m *SidebarModel) DragGroupOver(x, y int, src string) (GroupDrop, bool) {
	if d, ok := m.onDropLine(x, y); ok {
		return d.group, true
	}
	hit := m.HitTest(x, y)
	if _, inList := m.lineAt(x, y); inList && hit.Kind == HitNone && m.drop != nil {
		return m.drop.group, true
	}
	var mark *dropMark
	if (hit.Kind == HitGroup || hit.Kind == HitSession) && src != ClosedGroupName && hit.Group != ClosedGroupName && hit.Group != src {
		srcIdx, dstIdx := m.groupIndex(src), m.groupIndex(hit.Group)
		if srcIdx >= 0 && dstIdx >= 0 {
			before := dstIdx < srcIdx
			mark = &dropMark{key: "group\x00" + hit.Group, group: GroupDrop{Anchor: hit.Group, Before: before}}
			if !before {
				// The line goes above the next header, reusing its spacer.
				if dstIdx+1 < len(m.groups) {
					mark.key = "group\x00" + m.groups[dstIdx+1].Name
				} else {
					mark.key, mark.after = m.lastItemKey(hit.Group), true
				}
			}
		}
	}
	m.setDrop(mark)
	if mark == nil {
		return GroupDrop{}, false
	}
	return mark.group, true
}

// IsCollapsed reports whether group name is collapsed.
func (m *SidebarModel) IsCollapsed(name string) bool { return m.collapsed[name] }

// ClearDrop removes the drag marker.
func (m *SidebarModel) ClearDrop() { m.setDrop(nil) }

// ScrollBy scrolls the list by delta lines without moving the selection.
func (m *SidebarModel) ScrollBy(delta int) {
	m.vp.SetYOffset(m.vp.YOffset + delta)
}

// ListHeight is the number of list lines on screen, below the pane title.
func (m *SidebarModel) ListHeight() int { return m.vp.Height }

// MovedGroupOrder returns the group order after dropping group src at d: the
// current order with src moved, then groups remembered from earlier drags that
// aren't open now, so they keep their place when they come back.
func (m *SidebarModel) MovedGroupOrder(src string, d GroupDrop) []string {
	order := slices.DeleteFunc(slices.Clone(m.baseOrder), func(n string) bool { return n == src })
	at := slices.Index(order, d.Anchor)
	if at < 0 {
		return nil
	}
	if !d.Before {
		at++
	}
	order = slices.Insert(order, at, src)
	for _, n := range m.groupOrder {
		if !slices.Contains(order, n) {
			order = append(order, n)
		}
	}
	return order
}

// RenameGroup carries a group's collapse state, order and selection over to its
// new name after the tmux session was renamed.
func (m *SidebarModel) RenameGroup(oldName, newName string) {
	for _, mp := range []map[string]bool{m.collapsed, m.userToggled, m.savedCollapsed} {
		if v, ok := mp[oldName]; ok {
			mp[newName] = v
			delete(mp, oldName)
		}
	}
	for i, n := range m.groupOrder {
		if n == oldName {
			m.groupOrder[i] = newName
		}
	}
	if m.trackedGroup == oldName {
		m.trackedGroup = newName
	}
}

func (m *SidebarModel) setDrop(d *dropMark) {
	if d == nil && m.drop == nil {
		return
	}
	if d != nil && m.drop != nil && d.key == m.drop.key && d.after == m.drop.after && d.header == m.drop.header {
		m.drop = d
		return
	}
	m.drop = d
	m.syncViewport()
}

// onDropLine returns the current drop when (x, y) is on the insertion line, so
// the line shifting rows under a still pointer never flips the target.
func (m *SidebarModel) onDropLine(x, y int) (*dropMark, bool) {
	line, ok := m.lineAt(x, y)
	if !ok || m.drop == nil || m.lineItems[line] != dropLineOwner {
		return nil, false
	}
	return m.drop, true
}

func (m *SidebarModel) groupIndex(name string) int {
	for i, g := range m.groups {
		if g.Name == name {
			return i
		}
	}
	return -1
}

// lastItemKey returns the key of the last visible row of group name.
func (m *SidebarModel) lastItemKey(name string) string {
	key := "group\x00" + name
	for _, it := range m.flatItems {
		if it.groupName == name {
			key = it.key()
		}
	}
	return key
}

func (m *SidebarModel) paneSession(pane string) *session.Session {
	for _, s := range m.Sessions {
		if s.TmuxPaneID == pane {
			return s
		}
	}
	return nil
}

// movable reports a live row whose tmux window can be moved.
func movable(s *session.Session) bool {
	return s != nil && s.Status != session.StatusClosed && s.TmuxPaneID != ""
}

// sameWindow reports two sessions sharing a tmux window, which can't be ordered apart.
func sameWindow(a, b *session.Session) bool {
	if a == nil || b == nil {
		return false
	}
	return windowOf(a.TmuxTarget) == windowOf(b.TmuxTarget)
}

func windowOf(target string) string {
	if dot := strings.LastIndex(target, "."); dot >= 0 {
		return target[:dot]
	}
	return target
}
