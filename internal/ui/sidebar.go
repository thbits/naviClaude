package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tomhalo/naviclaude/internal/session"
	"github.com/tomhalo/naviclaude/internal/styles"
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
	Sessions     []*session.Session
	groups       []sessionGroup
	flatItems    []flatItem
	cursor       int
	width        int
	height       int
	scrollOffset int
	collapsed    map[string]bool
}

// NewSidebar creates a SidebarModel with the given dimensions.
func NewSidebar(width, height int) SidebarModel {
	return SidebarModel{
		width:     width,
		height:    height,
		collapsed: make(map[string]bool),
	}
}

// SetSessions rebuilds groups from the given sessions. Active sessions are
// grouped by TmuxSession. Closed sessions are placed in a "Closed" group.
func (m *SidebarModel) SetSessions(sessions []*session.Session) {
	m.Sessions = sessions
	m.rebuildGroups()
}

func (m *SidebarModel) rebuildGroups() {
	groupMap := make(map[string][]*session.Session)
	var closedSessions []*session.Session

	for _, s := range m.Sessions {
		if s.Status == session.StatusClosed {
			closedSessions = append(closedSessions, s)
		} else {
			groupMap[s.TmuxSession] = append(groupMap[s.TmuxSession], s)
		}
	}

	// Sort group names alphabetically.
	var names []string
	for name := range groupMap {
		names = append(names, name)
	}
	sort.Strings(names)

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

	m.rebuildFlatItems()
	m.clampCursor()
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

// SetSize updates the sidebar dimensions.
func (m *SidebarModel) SetSize(w, h int) {
	m.width = w
	m.height = h
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
			m.ensureVisible()
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
			m.ensureVisible()
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.flatItems) {
				item := m.flatItems[m.cursor]
				if item.isGroup {
					m.collapsed[item.groupName] = !m.collapsed[item.groupName]
					m.rebuildFlatItems()
					m.clampCursor()
				}
			}
		case "G":
			if len(m.flatItems) > 0 {
				m.cursor = len(m.flatItems) - 1
				m.ensureVisible()
			}
		case "g":
			m.cursor = 0
			m.ensureVisible()
		}
	}
	return m, nil
}

func (m *SidebarModel) ensureVisible() {
	// Each session takes 2 lines, group headers take 1.
	linePos := 0
	for i := 0; i < m.cursor && i < len(m.flatItems); i++ {
		if m.flatItems[i].isGroup {
			linePos++
		} else {
			linePos += 2
		}
	}

	if linePos < m.scrollOffset {
		m.scrollOffset = linePos
	}

	cursorLines := 1
	if m.cursor < len(m.flatItems) && !m.flatItems[m.cursor].isGroup {
		cursorLines = 2
	}
	if linePos+cursorLines > m.scrollOffset+m.height {
		m.scrollOffset = linePos + cursorLines - m.height
	}
}

// View renders the sidebar.
func (m SidebarModel) View() string {
	if len(m.flatItems) == 0 {
		empty := styles.EmptyState.Render("No sessions found")
		hint := styles.EmptyStateHint.Render("Press n to create one")
		content := lipgloss.JoinVertical(lipgloss.Left, empty, hint)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	var lines []string
	for i, item := range m.flatItems {
		isCursor := i == m.cursor
		if item.isGroup {
			lines = append(lines, m.renderGroupHeader(item.groupName, isCursor))
		} else {
			lines = append(lines, m.renderSessionItem(item.session, isCursor)...)
		}
	}

	// Apply scroll offset.
	if m.scrollOffset > 0 {
		if m.scrollOffset < len(lines) {
			lines = lines[m.scrollOffset:]
		} else {
			lines = nil
		}
	}

	// Truncate to height.
	if len(lines) > m.height {
		lines = lines[:m.height]
	}

	// Pad remaining lines to fill height.
	for len(lines) < m.height {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(m.width).Render(content)
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

	countStr := styles.SidebarGroupCount.Render(fmt.Sprintf(" %d", count))
	header := styles.SidebarGroupHeader.Render(fmt.Sprintf("%s %s", arrow, name)) + countStr

	if isCursor {
		header = styles.SidebarItemSelected.Width(m.width).Render(
			fmt.Sprintf("%s %s %s", arrow, name, fmt.Sprintf("%d", count)),
		)
	}

	return header
}

func (m SidebarModel) renderSessionItem(s *session.Session, isCursor bool) []string {
	icon := statusIcon(s.Status)
	relTime := relativeTime(time.Since(s.LastActivity))

	// First line: icon + project name + time
	prefix := "  "
	if isCursor {
		prefix = "\u25ba "
	}

	projectName := s.ProjectName
	if projectName == "" {
		projectName = s.ID[:8]
	}

	// Truncate project name if needed.
	maxNameWidth := m.width - len(prefix) - len(icon) - len(relTime) - 4
	if maxNameWidth < 0 {
		maxNameWidth = 10
	}
	if len(projectName) > maxNameWidth {
		projectName = projectName[:maxNameWidth]
	}

	nameStyled := styles.SidebarProjectName.Render(projectName)
	timeStyled := styles.SidebarTime.Render(relTime)
	gap := m.width - lipgloss.Width(prefix) - lipgloss.Width(icon) - lipgloss.Width(nameStyled) - lipgloss.Width(timeStyled) - 2
	if gap < 1 {
		gap = 1
	}

	line1 := fmt.Sprintf("%s%s %s%s%s", prefix, icon, nameStyled, strings.Repeat(" ", gap), timeStyled)

	// Second line: summary.
	summary := s.Summary
	maxSummary := m.width - 4
	if maxSummary < 0 {
		maxSummary = 10
	}
	if len(summary) > maxSummary {
		summary = summary[:maxSummary-3] + "..."
	}
	line2 := styles.SidebarSummary.Render(summary)

	if isCursor {
		line1 = styles.SidebarItemSelected.Width(m.width).Render(
			strings.TrimRight(line1, " "),
		)
		line2 = styles.SidebarItemSelected.Width(m.width).Render(
			strings.TrimRight(line2, " "),
		)
	}

	return []string{line1, line2}
}

func statusIcon(s session.SessionStatus) string {
	switch s {
	case session.StatusActive:
		return styles.StatusIconActive.Render("\u25cf")
	case session.StatusWaiting:
		return styles.StatusIconWaiting.Render("\u25ce")
	case session.StatusIdle:
		return styles.StatusIconIdle.Render("\u25cb")
	case session.StatusClosed:
		return styles.StatusIconClosed.Render("\u25cc")
	default:
		return " "
	}
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
