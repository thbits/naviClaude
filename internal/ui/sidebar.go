package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

// sessionGroup groups sessions by their tmux session name (or "Closed").
type sessionGroup struct {
	Name     string
	Sessions []*session.Session
}

// flatItem is an entry in the flattened visible list for cursor navigation.
type flatItem struct {
	isGroup   bool
	groupName string
	session   *session.Session
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
	activeCount       int     // cached count of non-closed sessions
	collapseAfterHrs  float64 // auto-collapse groups idle longer than this (hours)
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
	}
}

// SetCollapseAfterHours sets the threshold for auto-collapsing stale groups.
func (m *SidebarModel) SetCollapseAfterHours(hours float64) {
	m.collapseAfterHrs = hours
}

// ExpandAll sets search mode: all groups expand and stay expanded across
// subsequent SetSessions calls until RestoreCollapsed is called.
// Saves the current collapsed state so it can be restored later.
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
	count := 0
	for _, s := range sessions {
		if s.Status != session.StatusClosed {
			count++
		}
	}
	m.activeCount = count
	m.rebuildGroups()
}

// ActiveCount returns the cached count of non-closed sessions.
func (m *SidebarModel) ActiveCount() int {
	return m.activeCount
}

func (m *SidebarModel) rebuildGroups() {
	// Remember which session is selected so we can follow it after sorting.
	var selectedID string
	if sel := m.SelectedSession(); sel != nil {
		selectedID = sel.ID
	}

	groupMap := make(map[string][]*session.Session)
	var closedSessions []*session.Session

	for _, s := range m.Sessions {
		if s.Status == session.StatusClosed {
			closedSessions = append(closedSessions, s)
		} else {
			groupMap[s.TmuxSession] = append(groupMap[s.TmuxSession], s)
		}
	}

	// Sort sessions within each group by LastActivity descending (most recent first).
	for _, sessions := range groupMap {
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].LastActivity.After(sessions[j].LastActivity)
		})
	}
	sort.Slice(closedSessions, func(i, j int) bool {
		return closedSessions[i].LastActivity.After(closedSessions[j].LastActivity)
	})

	// Sort group names by the most recent session activity (most active group first).
	var names []string
	for name := range groupMap {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		gi := groupMap[names[i]]
		gj := groupMap[names[j]]
		if len(gi) == 0 || len(gj) == 0 {
			return len(gi) > len(gj)
		}
		return gi[0].LastActivity.After(gj[0].LastActivity)
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
			Name:     "Closed",
			Sessions: closedSessions,
		})
	}

	// During search, all groups stay expanded so results are selectable.
	if m.searchActive {
		for k := range m.collapsed {
			m.collapsed[k] = false
		}
		m.rebuildFlatItems()
		m.restoreCursorByID(selectedID)
		return
	}

	// Always collapse Closed group by default (unless user toggled it open).
	if !m.userToggled["Closed"] {
		m.collapsed["Closed"] = true
	}

	// Auto-collapse stale groups (unless user has manually toggled them).
	if m.collapseAfterHrs > 0 {
		cutoff := time.Now().Add(-time.Duration(m.collapseAfterHrs * float64(time.Hour)))
		for _, g := range m.groups {
			if g.Name == "Closed" {
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
	m.restoreCursorByID(selectedID)
}

func (m *SidebarModel) rebuildFlatItems() {
	m.flatItems = nil
	for _, g := range m.groups {
		m.flatItems = append(m.flatItems, flatItem{
			isGroup:   true,
			groupName: g.Name,
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

// restoreCursorByID moves the cursor to the session with the given ID after
// a list rebuild. If the session is no longer in the flat list, falls back
// to clampCursor so the index stays valid.
func (m *SidebarModel) restoreCursorByID(id string) {
	if id != "" {
		for i, item := range m.flatItems {
			if !item.isGroup && item.session != nil && item.session.ID == id {
				m.cursor = i
				m.syncViewport()
				return
			}
		}
	}
	m.clampCursor()
}

func (m *SidebarModel) clampCursor() {
	if len(m.flatItems) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.flatItems) {
		m.cursor = len(m.flatItems) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.syncViewport()
}

// syncViewport renders all items into the viewport content and scrolls to
// keep the cursor visible. Must be called after any change to flatItems,
// cursor, or dimensions so the viewport has up-to-date content before
// YOffset is adjusted.
func (m *SidebarModel) syncViewport() {
	var lines []string
	for i, item := range m.flatItems {
		isCursor := i == m.cursor
		if isCursor {
			m.cursorLineStart = len(lines)
		}
		if item.isGroup {
			rendered := m.renderGroupHeader(item.groupName, isCursor)
			// Split on embedded newlines -- Width().Render() may wrap text.
			lines = append(lines, strings.Split(rendered, "\n")...)
		} else {
			for _, rl := range m.renderSessionItem(item.session, isCursor) {
				// Each rendered line may itself contain embedded newlines
				// from Width().Render() wrapping.
				lines = append(lines, strings.Split(rl, "\n")...)
			}
		}
		if isCursor {
			m.cursorLineCount = len(lines) - m.cursorLineStart
		}
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
	m.scrollToCursor()
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

// SelectByID moves the cursor to the session with the given ID.
// Returns true if the session was found.
func (m *SidebarModel) SelectByID(id string) bool {
	for i, item := range m.flatItems {
		if !item.isGroup && item.session != nil && item.session.ID == id {
			m.cursor = i
			m.syncViewport()
			return true
		}
	}
	return false
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

// SetCursor moves the cursor to the given index.
func (m *SidebarModel) SetCursor(idx int) {
	m.cursor = idx
	m.syncViewport()
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
			if m.cursor < len(m.flatItems)-1 {
				m.cursor++
			}
			m.syncViewport()
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
			m.syncViewport()
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.flatItems) {
				item := m.flatItems[m.cursor]
				if item.isGroup {
					m.collapsed[item.groupName] = !m.collapsed[item.groupName]
					m.userToggled[item.groupName] = true // user explicitly toggled
					m.rebuildFlatItems()
					m.clampCursor()
				}
			}
		case "G":
			if len(m.flatItems) > 0 {
				m.cursor = len(m.flatItems) - 1
				m.syncViewport()
			}
		case "g":
			m.cursor = 0
			m.syncViewport()
		}
	}
	return m, nil
}

// scrollToCursor adjusts the viewport YOffset so the cursor item is visible.
// Uses cursorLineStart/cursorLineCount computed by syncViewport() from actual
// rendered output, avoiding assumptions about how many lines each item takes.
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

// View renders the sidebar.
func (m SidebarModel) View() string {
	// Render the "SESSIONS" header.
	activeCount := m.countActive()
	title := styles.SidebarTitle.Render("SESSIONS")
	countStr := styles.SidebarTitleCount.Render(fmt.Sprintf("%d active", activeCount))
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(countStr) - 2
	if gap < 1 {
		gap = 1
	}
	header := title + strings.Repeat(" ", gap) + countStr + " "

	if len(m.flatItems) == 0 {
		listHeight := m.vp.Height
		var emptyText, hintText string
		if m.searchActive {
			emptyText = "No matches found"
			hintText = "Try a different search query"
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

// countActive returns the cached count of non-closed sessions.
func (m SidebarModel) countActive() int {
	return m.activeCount
}

func (m SidebarModel) renderGroupHeader(name string, isCursor bool) string {
	arrow := "\u25bc" // down-pointing triangle (expanded)
	if m.collapsed[name] {
		arrow = "\u25b6" // right-pointing triangle (collapsed)
	}

	// Count sessions in this group.
	var count int
	for _, g := range m.groups {
		if g.Name == name {
			count = len(g.Sessions)
			break
		}
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
		return styles.SidebarItemSelected.Width(m.width - 1).Render(full)
	}

	// Normal group header: triangle + name left, count right-aligned
	left := styles.SidebarGroupHeader.Render(fmt.Sprintf("%s %s", arrow, name))
	right := styles.SidebarGroupCount.Render(fmt.Sprintf("%d", count))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m SidebarModel) renderSessionItem(s *session.Session, isCursor bool) []string {
	relTime := relativeTime(time.Since(s.LastActivity))

	displayName := shortPath(s.CWD, s.ProjectName)
	if displayName == "" && len(s.ID) >= 8 {
		displayName = s.ID[:8]
	} else if displayName == "" {
		displayName = "unknown"
	}

	// Truncate display name if needed.
	maxNameWidth := m.width - 10
	if maxNameWidth < 8 {
		maxNameWidth = 8
	}
	if len(displayName) > maxNameWidth {
		displayName = displayName[:maxNameWidth]
	}

	// Truncate summary.
	summary := s.Summary
	maxSummary := m.width - 6
	if maxSummary < 8 {
		maxSummary = 8
	}
	if len(summary) > maxSummary {
		summary = summary[:maxSummary-3] + "..."
	}

	isConfirmingKill := m.ConfirmKillTarget != "" && s.TmuxTarget == m.ConfirmKillTarget

	if isCursor {
		// Selected: blue left border, selection background.
		// Each segment gets explicit background to prevent ANSI reset bleeding.
		selBg := styles.ColorSelection
		if isConfirmingKill {
			selBg = styles.ColorBgHover
		}
		iconStyled := statusIconWithBg(s.Status, selBg)
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
			killLabel := lipgloss.NewStyle().Foreground(styles.ColorRed).Background(selBg).Bold(true).Render("Kill?")
			yKey := lipgloss.NewStyle().Foreground(styles.ColorFg).Background(selBg).Render(" y")
			slash := lipgloss.NewStyle().Foreground(styles.ColorGray).Background(selBg).Render("/")
			nKey := lipgloss.NewStyle().Foreground(styles.ColorFg).Background(selBg).Bold(true).Render("N")
			line2Content = killLabel + " " + yKey + slash + nKey
		} else {
			line2Content = summary
		}

		line2Style := lipgloss.NewStyle().
			Foreground(styles.ColorGray).
			Background(selBg).
			PaddingLeft(3).
			BorderLeft(true).
			BorderStyle(styles.SelectionIndicator).
			BorderForeground(borderFg)
		line2 := line2Style.Width(m.width - 1).Render(line2Content)
		return []string{line1, line2}
	}

	// Normal item.
	icon := statusIcon(s.Status)
	nameStyled := styles.SidebarProjectName.Render(displayName)
	timeStyled := styles.SidebarTime.Render(relTime)

	iconWidth := lipgloss.Width(icon)
	nameWidth := lipgloss.Width(nameStyled)
	timeWidth := lipgloss.Width(timeStyled)
	// 2 spaces indent + icon + 1 space + name + gap + time + 1 right pad
	gap := m.width - 2 - iconWidth - 1 - nameWidth - timeWidth - 1
	if gap < 1 {
		gap = 1
	}
	line1 := "  " + icon + " " + nameStyled + strings.Repeat(" ", gap) + timeStyled + " "
	line2 := styles.SidebarSummary.Render(summary)

	return []string{line1, line2}
}

// statusIconProps returns the foreground color and character for a status.
func statusIconProps(s session.SessionStatus) (lipgloss.Color, string) {
	switch s {
	case session.StatusActive:
		return styles.ColorGreen, styles.IconActive
	case session.StatusWaiting:
		return styles.ColorAmber, styles.IconWaiting
	case session.StatusIdle:
		return styles.ColorGray, styles.IconIdle
	case session.StatusClosed:
		return styles.ColorDim, styles.IconClosed
	default:
		return styles.ColorGray, " "
	}
}

func statusIcon(s session.SessionStatus) string {
	fg, ch := statusIconProps(s)
	return lipgloss.NewStyle().Foreground(fg).Render(ch)
}

// statusIconWithBg renders a status icon with an explicit background color,
// preventing ANSI reset bleeding when composed inside a styled container.
func statusIconWithBg(s session.SessionStatus, bg lipgloss.Color) string {
	fg, ch := statusIconProps(s)
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Render(ch)
}

// shortPath returns a display name derived from the CWD.
// Shows the last two path components (e.g. "git/opmed-charts") to hint
// that this is a directory. Falls back to projectName if CWD is empty.
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
