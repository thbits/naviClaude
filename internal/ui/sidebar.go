package ui

import (
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

// ClosedGroupName is the pseudo-group holding closed sessions; it is not a real
// tmux session, so callers mapping a group to a tmux target must special-case it.
const ClosedGroupName = "Closed"

// sessionGroup groups sessions by their tmux session name (or ClosedGroupName).
type sessionGroup struct {
	Name     string
	Sessions []*session.Session
}

// flatItem is an entry in the flattened visible list for cursor navigation.
type flatItem struct {
	isGroup   bool
	groupName string
	session   *session.Session
	// Group items only: cached so renderGroupHeader needn't rescan m.groups every frame.
	groupIdx   int
	groupCount int
}

// SidebarModel is the left panel showing sessions grouped by tmux session name.
type SidebarModel struct {
	Sessions          []*session.Session
	groups            []sessionGroup
	flatItems         []flatItem
	cursor            int
	width             int
	height            int
	collapsed         map[string]bool
	savedCollapsed    map[string]bool // snapshot before search expand-all
	userToggled       map[string]bool // tracks groups the user has manually toggled
	searchActive      bool            // when true, skip auto-collapse in rebuildGroups
	ConfirmKillTarget string          // tmux target of session pending kill confirmation
	vp                viewport.Model
	cursorLineStart   int     // actual line index where the cursor item starts
	cursorLineCount   int     // actual number of lines the cursor item occupies
	lineItems         []int   // flat-item index owning each content line; -1 for spacer lines
	collapseAfterHrs  float64 // auto-collapse groups idle longer than this (hours)
	groupSortOrder    string  // "name" or "activity"
	sessionSortOrder  string  // "name" or "activity"

	// Tracked selection: changed only by user navigation, so data refreshes
	// restore the cursor to it instead of overriding the user's choice.
	trackedPaneID string // tmux pane id ("%N"); stable even while a new session's ID settles
	trackedID     string // session ID the cursor should follow
	trackedTarget string // tmux target fallback (for placeholder sessions with no ID)
	trackedGroup  string // group header name fallback

	metrics      *session.SessionMetrics
	metricsID    string
	contextLimit ContextLimitFunc

	// Live detail for the selected session, keyed by session ID so a late
	// load for a previous selection never renders under the current one.
	activity       *session.SessionActivity
	activityID     string
	branches       map[string]session.GitRef     // cwd -> repo and branch, git repos only
	background     map[string]session.Background // session ID -> running subagents and shells
	changedFiles   []session.ChangedFile
	changedFilesID string

	breathingFrame int    // animation frame for breathing status dots
	spinnerView    string // spinner view string passed from app for loading state

	focused bool // whether the sidebar pane currently has keyboard focus
}

// NewSidebar creates a SidebarModel with the given dimensions.
func NewSidebar(width, height int) SidebarModel {
	vp := viewport.New(width, height-1) // -1 for header
	return SidebarModel{
		width:       width,
		height:      height,
		collapsed:   make(map[string]bool),
		userToggled: make(map[string]bool),
		vp:          vp,
		focused:     true,
	}
}

// SetCollapseAfterHours sets the threshold for auto-collapsing stale groups.
func (m *SidebarModel) SetCollapseAfterHours(hours float64) {
	m.collapseAfterHrs = hours
}

// SetGroupSortOrder sets the sort order for tmux session groups ("name" or "activity").
func (m *SidebarModel) SetGroupSortOrder(order string) {
	m.groupSortOrder = order
}

// SetSessionSortOrder sets the sort order for sessions within groups ("name" or "activity").
func (m *SidebarModel) SetSessionSortOrder(order string) {
	m.sessionSortOrder = order
}

// ExpandAll enters search mode: every group stays expanded across SetSessions
// until RestoreCollapsed brings back the saved collapsed state.
func (m *SidebarModel) ExpandAll() {
	m.searchActive = true
	m.savedCollapsed = make(map[string]bool)
	for k, v := range m.collapsed {
		m.savedCollapsed[k] = v
	}
	for k := range m.collapsed {
		m.collapsed[k] = false
	}
	m.rebuildFlatItems()
	m.clampCursor()
}

// RestoreCollapsed clears search mode and restores the collapsed state saved
// by ExpandAll, re-applying auto-collapse rules.
func (m *SidebarModel) RestoreCollapsed() {
	m.searchActive = false
	if m.savedCollapsed != nil {
		m.collapsed = m.savedCollapsed
		m.savedCollapsed = nil
	}
	m.rebuildGroups()
}

// SetSessions rebuilds groups from the given sessions. Active sessions are
// grouped by TmuxSession. Closed sessions are placed in a "Closed" group.
func (m *SidebarModel) SetSessions(sessions []*session.Session) {
	m.Sessions = sessions
	m.rebuildGroups()
}

// SetMetrics records the loaded metrics of the session with the given ID.
func (m *SidebarModel) SetMetrics(sessionID string, metrics *session.SessionMetrics) {
	m.metrics = metrics
	m.metricsID = sessionID
	m.syncViewport() // re-render so the context bar follows immediately
}

// ContextLimitFunc sizes a session's context window from its model id and
// peak token use; 0 means unknown.
type ContextLimitFunc func(s *session.Session, modelID string, tokensUsed int) int

// SetContextLimitFunc sets how context windows are sized for the ctx bar.
func (m *SidebarModel) SetContextLimitFunc(f ContextLimitFunc) { m.contextLimit = f }

// SetActivity updates the live activity of the session with the given ID.
func (m *SidebarModel) SetActivity(sessionID string, a *session.SessionActivity) {
	m.activity = a
	m.activityID = sessionID
	m.syncViewport()
}

// SetBranch records cwd's git repo and branch; inRepo false drops it so the
// repo line is omitted.
func (m *SidebarModel) SetBranch(cwd string, ref session.GitRef, inRepo bool) {
	if cwd == "" {
		return
	}
	if old, had := m.branches[cwd]; had == inRepo && old == ref {
		return
	}
	if m.branches == nil {
		m.branches = make(map[string]session.GitRef)
	}
	if inRepo {
		m.branches[cwd] = ref
	} else {
		delete(m.branches, cwd)
	}
	m.syncViewport()
}

// SetBackground records the running subagents and shells of every session, so
// rows other than the selected one show them too.
func (m *SidebarModel) SetBackground(counts map[string]session.Background) {
	if maps.Equal(m.background, counts) {
		return
	}
	m.background = counts
	m.syncViewport()
}

// SetChangedFiles records the files the session with the given ID edited, as
// loaded for the changed-files panel.
func (m *SidebarModel) SetChangedFiles(sessionID string, files []session.ChangedFile) {
	m.changedFiles = files
	m.changedFilesID = sessionID
	m.syncViewport()
}

// SetBreathingFrame updates the breathing animation frame and re-renders.
func (m *SidebarModel) SetBreathingFrame(frame int) {
	m.breathingFrame = frame
	m.syncViewport() // re-render to update dot brightness
}

// SetSpinnerView sets the spinner view string for the loading state.
func (m *SidebarModel) SetSpinnerView(view string) {
	m.spinnerView = view
}

// SetFocused marks whether the sidebar pane has keyboard focus, re-rendering on change.
// SetSize doesn't render, so unfocused-resize dimming relies on this re-sync; it fires
// reliably because View() calls it on a throwaway copy, so the stored focus never flips.
func (m *SidebarModel) SetFocused(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	m.syncViewport()
}

func (m *SidebarModel) rebuildGroups() {
	// Restore from the tracked selection, not the cursor index, so refreshes never move it.

	groupMap := make(map[string][]*session.Session)
	var closedSessions []*session.Session

	for _, s := range m.Sessions {
		if s.Status == session.StatusClosed {
			closedSessions = append(closedSessions, s)
		} else {
			groupMap[s.TmuxSession] = append(groupMap[s.TmuxSession], s)
		}
	}

	// Sort sessions within each group.
	activitySort := func(sessions []*session.Session) {
		sort.Slice(sessions, func(i, j int) bool {
			ti := sessions[i].LastActivity
			tj := sessions[j].LastActivity
			if !ti.Equal(tj) {
				return ti.After(tj)
			}
			return sessions[i].ID < sessions[j].ID
		})
	}
	nameSort := func(sessions []*session.Session) {
		sort.Slice(sessions, func(i, j int) bool {
			ni := strings.ToLower(sessionTitle(sessions[i]))
			nj := strings.ToLower(sessionTitle(sessions[j]))
			if ni != nj {
				return ni < nj
			}
			ti := sessions[i].LastActivity
			tj := sessions[j].LastActivity
			if !ti.Equal(tj) {
				return ti.After(tj)
			}
			return sessions[i].ID < sessions[j].ID
		})
	}

	sortFn := nameSort
	if m.sessionSortOrder == "activity" {
		sortFn = activitySort
	}
	for _, sessions := range groupMap {
		sortFn(sessions)
		// Sessions that need the user float above the configured order.
		sort.SliceStable(sessions, func(i, j int) bool {
			return needsUser(sessions[i]) && !needsUser(sessions[j])
		})
	}
	// Closed sessions always sort by activity (slug context is sparse).
	activitySort(closedSessions)

	// Sort group names.
	var names []string
	for name := range groupMap {
		names = append(names, name)
	}
	if m.groupSortOrder == "activity" {
		// Most-recent activity first, name as tiebreaker. Scan the whole group:
		// Sessions[0] is only the newest under the activity session-sort.
		recent := make(map[string]time.Time, len(groupMap))
		for name, sessions := range groupMap {
			recent[name] = groupRecentActivity(sessions)
		}
		sort.Slice(names, func(i, j int) bool {
			ti := recent[names[i]]
			tj := recent[names[j]]
			if !ti.Equal(tj) {
				return ti.After(tj)
			}
			return names[i] < names[j]
		})
	} else {
		sort.Strings(names)
	}
	// Groups holding a session that needs the user come first.
	waitingGroup := make(map[string]bool, len(groupMap))
	for name, sessions := range groupMap {
		for _, s := range sessions {
			if needsUser(s) {
				waitingGroup[name] = true
				break
			}
		}
	}
	sort.SliceStable(names, func(i, j int) bool {
		return waitingGroup[names[i]] && !waitingGroup[names[j]]
	})

	m.groups = nil
	for _, name := range names {
		m.groups = append(m.groups, sessionGroup{
			Name:     name,
			Sessions: groupMap[name],
		})
	}
	if len(closedSessions) > 0 {
		m.groups = append(m.groups, sessionGroup{
			Name:     ClosedGroupName,
			Sessions: closedSessions,
		})
	}

	// During search, all groups stay expanded so results are selectable.
	if m.searchActive {
		for k := range m.collapsed {
			m.collapsed[k] = false
		}
		m.rebuildFlatItems()
		m.restoreCursor(m.trackedID, m.trackedTarget, m.trackedGroup)
		return
	}

	// Always collapse Closed group by default (unless user toggled it open).
	if !m.userToggled[ClosedGroupName] {
		m.collapsed[ClosedGroupName] = true
	}

	// Auto-collapse stale groups (unless user has manually toggled them).
	if m.collapseAfterHrs > 0 {
		cutoff := time.Now().Add(-time.Duration(m.collapseAfterHrs * float64(time.Hour)))
		for _, g := range m.groups {
			if g.Name == ClosedGroupName {
				continue
			}
			if m.userToggled[g.Name] {
				continue // user has explicitly toggled this group; respect their choice
			}
			allStale := true
			for _, s := range g.Sessions {
				if s.LastActivity.After(cutoff) {
					allStale = false
					break
				}
			}
			if allStale {
				m.collapsed[g.Name] = true
			} else {
				m.collapsed[g.Name] = false
			}
		}
	}

	m.rebuildFlatItems()
	m.restoreCursor(m.trackedID, m.trackedTarget, m.trackedGroup)
}

func (m *SidebarModel) rebuildFlatItems() {
	m.flatItems = nil
	for gi, g := range m.groups {
		m.flatItems = append(m.flatItems, flatItem{
			isGroup:    true,
			groupName:  g.Name,
			groupIdx:   gi,
			groupCount: len(g.Sessions),
		})
		if !m.collapsed[g.Name] {
			for _, s := range g.Sessions {
				m.flatItems = append(m.flatItems, flatItem{
					session: s,
				})
			}
		}
	}
}

// restoreCursor moves the cursor back to the tracked item after a list rebuild,
// matching by pane, ID+target, ID, target, then group header.
func (m *SidebarModel) restoreCursor(id, tmuxTarget, groupName string) {
	// A live pane keeps its id while detection refines the session ID, so it
	// outranks the ID: a new session can briefly borrow another session's ID.
	if m.trackedPaneID != "" {
		for i, item := range m.flatItems {
			if !item.isGroup && item.session != nil && item.session.TmuxPaneID == m.trackedPaneID {
				m.cursor = i
				m.updateTracked()
				m.syncViewport()
				return
			}
		}
	}
	// Next best: both ID and target (handles duplicate IDs across tmux sessions).
	if id != "" && tmuxTarget != "" {
		for i, item := range m.flatItems {
			if !item.isGroup && item.session != nil && item.session.ID == id && item.session.TmuxTarget == tmuxTarget {
				m.cursor = i
				m.updateTracked()
				m.syncViewport()
				return
			}
		}
	}
	// Fall back to ID only.
	if id != "" {
		for i, item := range m.flatItems {
			if !item.isGroup && item.session != nil && item.session.ID == id {
				m.cursor = i
				m.updateTracked()
				m.syncViewport()
				return
			}
		}
	}
	// Fall back to tmux target only (for placeholder sessions with no ID yet).
	if tmuxTarget != "" {
		for i, item := range m.flatItems {
			if !item.isGroup && item.session != nil && item.session.TmuxTarget == tmuxTarget {
				m.cursor = i
				m.updateTracked()
				m.syncViewport()
				return
			}
		}
	}
	// Fall back to matching a group header by name.
	if groupName != "" {
		for i, item := range m.flatItems {
			if item.isGroup && item.groupName == groupName {
				m.cursor = i
				m.updateTracked()
				m.syncViewport()
				return
			}
		}
	}
	// Not found: clamp, then snap to a session row so the cursor doesn't strand on a header.
	m.clampCursor()
	m.snapToNearestSession()
}

// snapToNearestSession moves the cursor off a group header onto the closest
// session row, preferring the next row; with no session rows it stays put.
func (m *SidebarModel) snapToNearestSession() {
	n := len(m.flatItems)
	if n == 0 {
		return
	}
	if m.cursor < 0 || m.cursor >= n {
		return
	}
	if !m.flatItems[m.cursor].isGroup {
		return // already on a session row
	}
	for radius := 1; radius < n; radius++ {
		if next := m.cursor + radius; next < n && !m.flatItems[next].isGroup {
			m.cursor = next
			m.syncViewport()
			return
		}
		if prev := m.cursor - radius; prev >= 0 && !m.flatItems[prev].isGroup {
			m.cursor = prev
			m.syncViewport()
			return
		}
	}
}

func (m *SidebarModel) clampCursor() {
	if len(m.flatItems) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.flatItems) {
		m.cursor = len(m.flatItems) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.syncViewport()
}

// syncViewport re-renders all items into the viewport and scrolls the cursor into
// view. Call it after any change to flatItems or the cursor.
func (m *SidebarModel) syncViewport() {
	var lines []string
	// Fresh slice: View() runs on a copy that shares this backing array.
	m.lineItems = make([]int, 0, len(m.lineItems))
	for i, item := range m.flatItems {
		isCursor := i == m.cursor
		if isCursor {
			m.cursorLineStart = len(lines)
		}
		if item.isGroup {
			rendered := m.renderGroupHeader(item.groupName, item.groupCount, item.groupIdx, isCursor)
			// Split on embedded newlines -- Width().Render() may wrap text.
			for j, l := range strings.Split(rendered, "\n") {
				owner := i
				if j == 0 && item.groupIdx > 0 {
					owner = -1 // the blank spacer renderGroupHeader prepends
				}
				lines = append(lines, l)
				m.lineItems = append(m.lineItems, owner)
			}
		} else {
			for _, rl := range m.renderSessionItem(item.session, isCursor) {
				// Each rendered line may itself contain embedded newlines
				// from Width().Render() wrapping.
				for _, l := range strings.Split(rl, "\n") {
					lines = append(lines, l)
					m.lineItems = append(m.lineItems, i)
				}
			}
		}
		if isCursor {
			m.cursorLineCount = len(lines) - m.cursorLineStart
		}
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
	m.scrollToCursor()
}

// SidebarHitKind classifies what lies under a sidebar coordinate.
type SidebarHitKind int

const (
	// HitNone is the pane title, a spacer line, or empty space.
	HitNone SidebarHitKind = iota
	// HitSession is a session row.
	HitSession
	// HitGroup is a group header.
	HitGroup
)

// SidebarHit describes the item under a sidebar coordinate.
type SidebarHit struct {
	Kind    SidebarHitKind
	Index   int // flat-item index, valid for HitSession and HitGroup
	Session *session.Session
	Group   string
}

// HitTest reports the item at sidebar-local (x, y), y=0 being the pane title,
// from the line map syncViewport built for the last render (scroll included).
func (m *SidebarModel) HitTest(x, y int) SidebarHit {
	// The title wraps to two lines on very narrow panes.
	headerH := lipgloss.Height(m.renderHeader())
	if x < 0 || x >= m.width || y < headerH || y >= headerH+m.vp.Height {
		return SidebarHit{}
	}
	line := m.vp.YOffset + y - headerH
	if line < 0 || line >= len(m.lineItems) {
		return SidebarHit{}
	}
	idx := m.lineItems[line]
	if idx < 0 || idx >= len(m.flatItems) {
		return SidebarHit{}
	}
	item := m.flatItems[idx]
	if item.isGroup {
		return SidebarHit{Kind: HitGroup, Index: idx, Group: item.groupName}
	}
	return SidebarHit{Kind: HitSession, Index: idx, Session: item.session}
}

// MoveCursor moves the selection by delta rows, clamped to the list.
func (m *SidebarModel) MoveCursor(delta int) {
	m.cursor += delta
	if m.cursor > len(m.flatItems)-1 {
		m.cursor = len(m.flatItems) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.updateTracked()
	m.syncViewport()
}

// ToggleGroupAtCursor collapses or expands the group header under the cursor;
// a no-op when the cursor is on a session row.
func (m *SidebarModel) ToggleGroupAtCursor() {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return
	}
	item := m.flatItems[m.cursor]
	if !item.isGroup {
		return
	}
	m.collapsed[item.groupName] = !m.collapsed[item.groupName]
	m.userToggled[item.groupName] = true // user explicitly toggled
	m.rebuildFlatItems()
	m.clampCursor()
	// Re-anchor the tracked selection to wherever the cursor landed; otherwise
	// the next data refresh re-runs restoreCursor against a stale target.
	m.updateTracked()
}

// SelectedSession returns the session at the current cursor, or nil if the
// cursor is on a group header or the list is empty.
func (m *SidebarModel) SelectedSession() *session.Session {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return nil
	}
	item := m.flatItems[m.cursor]
	if item.isGroup {
		return nil
	}
	return item.session
}

// SelectedGroupName returns the group name if the cursor is on a group header,
// or empty string otherwise.
func (m *SidebarModel) SelectedGroupName() string {
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return ""
	}
	item := m.flatItems[m.cursor]
	if item.isGroup {
		return item.groupName
	}
	return ""
}

// GroupSessions returns the sessions belonging to a named group.
func (m *SidebarModel) GroupSessions(name string) []*session.Session {
	for _, g := range m.groups {
		if g.Name == name {
			return g.Sessions
		}
	}
	return nil
}

// SelectByID moves the cursor to the session whose Key() matches key.
// Returns true if the session was found.
func (m *SidebarModel) SelectByID(key string) bool {
	for i, item := range m.flatItems {
		if !item.isGroup && item.session != nil && item.session.Key() == key {
			m.cursor = i
			m.updateTracked()
			m.syncViewport()
			return true
		}
	}
	return false
}

// SelectByTarget moves the cursor to the session with the given tmux target,
// reporting whether it was found; used for placeholders that have no ID yet.
func (m *SidebarModel) SelectByTarget(target string) bool {
	if target == "" {
		return false
	}
	for i, item := range m.flatItems {
		if !item.isGroup && item.session != nil && item.session.TmuxTarget == target {
			m.cursor = i
			m.updateTracked()
			m.syncViewport()
			return true
		}
	}
	return false
}

// ExpandGroup expands the named group (e.g. to show a new session in it) and
// marks it user-toggled so auto-collapse leaves it open.
func (m *SidebarModel) ExpandGroup(name string) {
	if name == "" || !m.collapsed[name] {
		return
	}
	m.collapsed[name] = false
	m.userToggled[name] = true
	m.rebuildFlatItems()
	m.restoreCursor(m.trackedID, m.trackedTarget, m.trackedGroup)
}

// ToggledGroups returns the collapsed state of every group the user has manually
// toggled, keyed by group name. Used to persist group open/closed state.
func (m *SidebarModel) ToggledGroups() map[string]bool {
	out := make(map[string]bool, len(m.userToggled))
	for name := range m.userToggled {
		out[name] = m.collapsed[name]
	}
	return out
}

// SeedToggledGroups loads persisted group state as if the user had toggled each
// group, exempting them from auto-collapse. Call before the first SetSessions.
func (m *SidebarModel) SeedToggledGroups(groups map[string]bool) {
	for name, collapsed := range groups {
		m.userToggled[name] = true
		m.collapsed[name] = collapsed
	}
}

// FlatItem is a public wrapper for flat list items to allow external iteration.
type FlatItem struct {
	IsGroup bool
	Session *session.Session
}

// FlatItems returns the flattened visible list for external iteration.
func (m *SidebarModel) FlatItems() []FlatItem {
	items := make([]FlatItem, len(m.flatItems))
	for i, fi := range m.flatItems {
		items[i] = FlatItem{IsGroup: fi.isGroup, Session: fi.session}
	}
	return items
}

// Cursor returns the current cursor index.
func (m *SidebarModel) Cursor() int {
	return m.cursor
}

// SetCursor moves the cursor to the given index and updates the tracked selection.
func (m *SidebarModel) SetCursor(idx int) {
	m.cursor = idx
	m.updateTracked()
	m.syncViewport()
}

// updateTracked saves the current cursor's identity so rebuildGroups can
// restore it after re-sorting. Only call this after user-initiated navigation.
func (m *SidebarModel) updateTracked() {
	m.trackedPaneID = ""
	m.trackedID = ""
	m.trackedTarget = ""
	m.trackedGroup = ""
	if m.cursor < 0 || m.cursor >= len(m.flatItems) {
		return
	}
	item := m.flatItems[m.cursor]
	if item.isGroup {
		m.trackedGroup = item.groupName
	} else if item.session != nil {
		m.trackedPaneID = item.session.TmuxPaneID
		m.trackedID = item.session.ID
		m.trackedTarget = item.session.TmuxTarget
	}
}

// SetSize updates the sidebar dimensions.
func (m *SidebarModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	listHeight := h - 1 // header takes 1 line
	if listHeight < 1 {
		listHeight = 1
	}
	m.vp.Width = w
	m.vp.Height = listHeight
}

// Init satisfies the tea.Model interface.
func (m SidebarModel) Init() tea.Cmd {
	return nil
}

// Update handles navigation keys.
func (m SidebarModel) Update(msg tea.Msg) (SidebarModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			m.MoveCursor(1)
		case "k", "up":
			m.MoveCursor(-1)
		case "enter":
			m.ToggleGroupAtCursor()
		case "G":
			if len(m.flatItems) > 0 {
				m.cursor = len(m.flatItems) - 1
				m.updateTracked()
				m.syncViewport()
			}
		case "g":
			m.cursor = 0
			m.updateTracked()
			m.syncViewport()
		}
	}
	return m, nil
}

// scrollToCursor adjusts the viewport YOffset so the cursor item is visible, using
// the line span syncViewport measured since items vary in height.
func (m *SidebarModel) scrollToCursor() {
	linePos := m.cursorLineStart
	cursorLines := m.cursorLineCount
	if cursorLines < 1 {
		cursorLines = 1
	}

	yOff := m.vp.YOffset

	// Scroll up if cursor is above the viewport.
	if linePos < yOff {
		yOff = linePos
	}

	// Scroll down if cursor is below the viewport.
	if linePos+cursorLines > yOff+m.vp.Height {
		yOff = linePos + cursorLines - m.vp.Height
	}

	m.vp.SetYOffset(yOff)
}

// renderHeader renders the pane title: a lit reverse-video bar when focused, a
// dim title otherwise. Both fill to m.width so focus never changes the pane width.
func (m SidebarModel) renderHeader() string {
	// No session count here: the title bar's status summary already shows it.
	if m.focused {
		return styles.PaneTitleActive.Width(m.width).Render(" ▸ SESSIONS")
	}
	title := styles.PaneTitleInactive.Render("SESSIONS")
	if pad := m.width - lipgloss.Width(title); pad > 0 {
		title += strings.Repeat(" ", pad)
	}
	return title
}

// View renders the sidebar.
func (m SidebarModel) View() string {
	header := m.renderHeader()

	if len(m.flatItems) == 0 {
		listHeight := m.vp.Height
		var emptyText, hintText string
		if m.searchActive {
			emptyText = "No matches found"
			hintText = "Try a different search query"
		} else if m.spinnerView != "" {
			emptyText = m.spinnerView + " Loading sessions..."
			hintText = ""
		} else {
			emptyText = "No sessions found"
			hintText = "Press n to create one"
		}
		empty := styles.EmptyState.Render(emptyText)
		hint := styles.EmptyStateHint.Render(hintText)
		body := lipgloss.JoinVertical(lipgloss.Left, empty, hint)
		body = lipgloss.Place(m.width, listHeight, lipgloss.Center, lipgloss.Center, body)
		return lipgloss.JoinVertical(lipgloss.Left, header, body)
	}

	// Content and scroll position are kept in sync by syncViewport()
	// which is called on every cursor/data change. Just render.
	return lipgloss.JoinVertical(lipgloss.Left, header, m.vp.View())
}

// needsUser reports a session blocked on the user (approval or question).
func needsUser(s *session.Session) bool { return s.Status == session.StatusWaiting }

// groupRecentActivity returns the newest LastActivity in a group; an empty group
// yields the zero Time, which sorts last.
func groupRecentActivity(sessions []*session.Session) time.Time {
	var newest time.Time
	for _, s := range sessions {
		if s.LastActivity.After(newest) {
			newest = s.LastActivity
		}
	}
	return newest
}

// renderGroupHeader renders a group header row; count and idx come from the
// flatItems walk to avoid rescanning m.groups every frame.
func (m SidebarModel) renderGroupHeader(name string, count, idx int, isCursor bool) string {
	arrow := "\u25bc" // down-pointing triangle (expanded)
	if m.collapsed[name] {
		arrow = "\u25b6" // right-pointing triangle (collapsed)
	}

	if isCursor {
		content := fmt.Sprintf("%s %s", arrow, name)
		countRendered := fmt.Sprintf("%d", count)
		innerWidth := m.width - 2 // PaddingLeft(1) inside Width; border is outside
		if innerWidth < 10 {
			innerWidth = 10
		}
		gap := innerWidth - lipgloss.Width(content) - lipgloss.Width(countRendered)
		if gap < 1 {
			gap = 1
		}
		full := content + strings.Repeat(" ", gap) + countRendered
		rendered := styles.SidebarItemSelected.Width(m.width - 1).Render(full)
		if idx > 0 {
			return "\n" + rendered
		}
		return rendered
	}

	// Normal group header: triangle + name left, count right-aligned. Dim toward
	// DimText when the pane is unfocused.
	ghStyle := styles.SidebarGroupHeader
	gcStyle := styles.SidebarGroupCount
	if !m.focused {
		ghStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText).PaddingLeft(1)
		gcStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText)
	}
	left := ghStyle.Render(fmt.Sprintf("%s %s", arrow, name))
	right := gcStyle.Render(fmt.Sprintf("%d", count))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	if idx > 0 {
		return "\n" + line
	}
	return line
}

func (m SidebarModel) renderSessionItem(s *session.Session, isCursor bool) []string {
	relTime := relativeTime(time.Since(s.LastActivity))

	displayName := sessionTitle(s)
	if displayName == "" && len(s.ID) >= 8 {
		displayName = s.ID[:8]
	} else if displayName == "" {
		displayName = "unknown"
	}

	// Truncate display name if needed. Rune/display-width aware so multibyte
	// names are never split mid-rune; an ellipsis marks the cut.
	maxNameWidth := m.width - 10
	if maxNameWidth < 8 {
		maxNameWidth = 8
	}
	displayName = truncateDisplay(displayName, maxNameWidth)

	// Truncate summary the same way for consistency.
	summary := s.Summary
	maxSummary := m.width - 6
	if maxSummary < 8 {
		maxSummary = 8
	}
	summary = truncateDisplay(summary, maxSummary)
	// A session with no prompt yet would otherwise leave a blank row.
	noPrompt := summary == ""
	if noPrompt {
		summary = "no prompt yet"
	}

	isConfirmingKill := m.ConfirmKillTarget != "" && s.TmuxTarget == m.ConfirmKillTarget

	if isCursor {
		// Selected: blue left border, selection background.
		// Each segment gets explicit background to prevent ANSI reset bleeding.
		selBg := styles.ColorSelection
		if isConfirmingKill {
			selBg = styles.ColorBgHover
		}
		iconStyled := sessionIconWithBg(s, selBg, m.breathingFrame)
		nameStyled := lipgloss.NewStyle().Foreground(styles.ColorBlue).Background(selBg).Bold(true).Render(displayName)
		timeStyled := lipgloss.NewStyle().Foreground(styles.ColorGray).Background(selBg).Render(relTime)

		innerWidth := m.width - 2 // PaddingLeft(1) inside Width; border is outside
		if innerWidth < 10 {
			innerWidth = 10
		}
		iconWidth := lipgloss.Width(iconStyled)
		nameWidth := lipgloss.Width(nameStyled)
		timeWidth := lipgloss.Width(timeStyled)
		gap := innerWidth - iconWidth - 1 - nameWidth - timeWidth
		if gap < 1 {
			gap = 1
		}
		spacer := lipgloss.NewStyle().Background(selBg).Render(" ")
		gapStr := lipgloss.NewStyle().Background(selBg).Render(strings.Repeat(" ", gap))
		line1Content := iconStyled + spacer + nameStyled + gapStr + timeStyled

		borderFg := styles.ColorBlue
		if isConfirmingKill {
			borderFg = styles.ColorRed
		}
		line1Style := lipgloss.NewStyle().
			Foreground(styles.ColorBlue).
			Background(selBg).
			Bold(true).
			PaddingLeft(1).
			BorderLeft(true).
			BorderStyle(styles.SelectionIndicator).
			BorderForeground(borderFg)
		line1 := line1Style.Width(m.width - 1).Render(line1Content)

		var line2Content string
		if isConfirmingKill {
			dimBg := styles.ColorSelectionDim
			killLabel := lipgloss.NewStyle().Foreground(styles.ColorRed).Background(dimBg).Bold(true).Render("Kill?")
			yKey := lipgloss.NewStyle().Foreground(styles.ColorFg).Background(dimBg).Render(" y")
			slash := lipgloss.NewStyle().Foreground(styles.ColorGray).Background(dimBg).Render("/")
			nKey := lipgloss.NewStyle().Foreground(styles.ColorFg).Background(dimBg).Bold(true).Render("N")
			line2Content = killLabel + " " + yKey + slash + nKey
		} else if noPrompt {
			line2Content = lipgloss.NewStyle().Foreground(styles.ColorDimText).Background(styles.ColorSelectionDim).Italic(true).Render(summary)
		} else {
			line2Content = summary
		}

		line2Style := lipgloss.NewStyle().
			Foreground(styles.ColorGray).
			Background(styles.ColorSelectionDim).
			PaddingLeft(3).
			BorderLeft(true).
			BorderStyle(styles.SelectionIndicator).
			BorderForeground(borderFg)
		line2 := line2Style.Width(m.width - 1).Render(line2Content)

		var extraLines []string
		dimBg := styles.ColorSelectionDim
		extraStyle := lipgloss.NewStyle().
			Background(dimBg).
			PaddingLeft(3).
			BorderLeft(true).
			BorderStyle(styles.SelectionIndicator).
			BorderForeground(borderFg).
			Width(m.width - 1)

		if m.width >= 35 {
			extraLines = append(extraLines, m.renderDetailLines(s, dimBg, extraStyle)...)
		}

		result := []string{line1, line2}
		result = append(result, extraLines...)
		return result
	}

	// Normal item. The status icon keeps its semantic color even when dimmed so
	// session status stays glanceable across panes; only the text dims.
	icon := sessionIcon(s, m.breathingFrame)
	nameStyle := styles.SidebarProjectName
	timeStyle := styles.SidebarTime
	summaryStyle := styles.SidebarSummary
	if !m.focused {
		nameStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText)
		timeStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText)
		summaryStyle = lipgloss.NewStyle().Foreground(styles.ColorDimText).PaddingLeft(4)
	}
	nameStyled := nameStyle.Render(displayName)
	timeStyled := timeStyle.Render(relTime)

	iconWidth := lipgloss.Width(icon)
	nameWidth := lipgloss.Width(nameStyled)
	timeWidth := lipgloss.Width(timeStyled)
	// 2 spaces indent + icon + 1 space + name + gap + time + 1 right pad
	gap := m.width - 2 - iconWidth - 1 - nameWidth - timeWidth - 1
	if gap < 1 {
		gap = 1
	}
	line1 := "  " + icon + " " + nameStyled + strings.Repeat(" ", gap) + timeStyled + " "
	if noPrompt {
		summaryStyle = summaryStyle.Foreground(styles.ColorDimText).Italic(true)
	}
	line2 := summaryStyle.Render(summary)

	if bg := m.background[s.ID]; s.ID != "" && s.Status != session.StatusClosed && bg != (session.Background{}) {
		// Same indent as the summary; the selected row shows this in its activity line.
		return []string{line1, line2, "    " + renderSegments(backgroundSegments(bg), "", m.width-5)}
	}
	return []string{line1, line2}
}

// statusIconProps returns the foreground color and character for a status.
// The frame parameter drives the breathing animation for active/waiting dots.
func statusIconProps(s session.SessionStatus, frame int) (lipgloss.Color, string) {
	switch s {
	case session.StatusActive:
		// Breathing: toggle every 2 frames (800ms cycle)
		if frame%4 < 2 {
			return styles.ColorGreen, styles.IconActive
		}
		return styles.ColorBorder, styles.IconActive
	case session.StatusWaiting:
		// Slower breathing: toggle every 3 frames (1200ms cycle)
		if frame%6 < 3 {
			return styles.ColorAmber, styles.IconWaiting
		}
		return styles.ColorGray, styles.IconWaiting
	case session.StatusIdle:
		return styles.ColorGray, styles.IconIdle
	case session.StatusClosed:
		return styles.ColorDim, styles.IconClosed
	default:
		return styles.ColorGray, " "
	}
}

// sessionIconProps is statusIconProps plus the hook-raised "finished while you
// were away" state, which only applies to an otherwise idle session.
func sessionIconProps(s *session.Session, frame int) (lipgloss.Color, string) {
	if s.Status == session.StatusIdle && s.Attention == attention.Done {
		return styles.ColorBlue, styles.IconDone
	}
	return statusIconProps(s.Status, frame)
}

func sessionIcon(s *session.Session, frame int) string {
	fg, ch := sessionIconProps(s, frame)
	return lipgloss.NewStyle().Foreground(fg).Render(ch)
}

func sessionIconWithBg(s *session.Session, bg lipgloss.Color, frame int) string {
	fg, ch := sessionIconProps(s, frame)
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Render(ch)
}

// sessionTitle returns the best display name for a session:
// user override > slug > shortPath fallback.
func sessionTitle(s *session.Session) string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	if s.Slug != "" {
		return s.Slug
	}
	return shortPath(s.CWD, s.ProjectName)
}

func shortPath(cwd, projectName string) string {
	if cwd == "" {
		return projectName
	}
	parts := strings.Split(filepath.ToSlash(cwd), "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	if len(parts) == 1 && parts[0] != "" {
		return parts[0]
	}
	return projectName
}

// relativeTime converts a duration into a short human-readable string.
func relativeTime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// renderDetailLines renders the selected session's live detail: activity
// always, then context fill and repo state when known.
func (m SidebarModel) renderDetailLines(s *session.Session, bg lipgloss.Color, box lipgloss.Style) []string {
	// Width(m.width-1) includes PaddingLeft(3); the border sits outside it.
	maxWidth := m.width - 4
	var act *session.SessionActivity
	if s.ID != "" && m.activityID == s.ID {
		act = m.activity
	}
	lines := []string{box.Render(renderSegments(activitySegments(s, act, time.Now()), bg, maxWidth))}

	var metrics *session.SessionMetrics
	if s.ID != "" && m.metricsID == s.ID {
		metrics = m.metrics
	}
	if used, limit := m.contextFill(s, metrics, act); used > 0 {
		if ctx := ctxLine(used, limit, bg, maxWidth); ctx != "" {
			lines = append(lines, box.Render(ctx))
		}
	}

	if ref, ok := m.branches[s.CWD]; ok && s.CWD != "" {
		var files []session.ChangedFile
		if s.ID != "" && m.changedFilesID == s.ID {
			files = m.changedFiles
		}
		lines = append(lines, box.Render(renderSegments(repoSegments(ref, files, maxWidth), bg, maxWidth)))
	}
	return lines
}

// contextFill returns current context use and window size, resolving the limit
// per render since the tail read can outgrow (or re-model) the one-off full scan.
func (m SidebarModel) contextFill(s *session.Session, metrics *session.SessionMetrics, act *session.SessionActivity) (used, limit int) {
	var modelID string
	peak := 0
	if metrics != nil {
		used, peak, modelID = metrics.TokensUsed, metrics.TokensUsed, metrics.ModelID
		limit = metrics.ContextLimit
	}
	if act != nil {
		if act.TokensUsed > 0 {
			used = act.TokensUsed // the tail read is fresher than the full scan
		}
		peak = max(peak, act.TokensUsed)
		if act.Model != "" {
			modelID = act.Model
		}
	}
	if m.contextLimit != nil {
		if l := m.contextLimit(s, modelID, peak); l > 0 {
			limit = l
		}
	}
	return used, limit
}
