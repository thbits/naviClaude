package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

// PreviewModel is the right panel that shows captured terminal content for the
// selected session.
type PreviewModel struct {
	content     string
	session     *session.Session
	metrics     *session.SessionMetrics
	passthrough bool
	viewport    viewport.Model
	width       int
	height      int
}

// NewPreview creates a PreviewModel with the given dimensions.
func NewPreview(width, height int) PreviewModel {
	vp := viewport.New(width, height-2) // subtract 2 for header + border-bottom line
	return PreviewModel{
		width:    width,
		height:   height,
		viewport: vp,
	}
}

// SetContent updates the terminal capture displayed in the viewport.
// Lines wider than the viewport are truncated using ANSI-aware truncation.
// For live sessions, the viewport auto-scrolls to the bottom to follow output.
// The scroll position is only preserved when the user has manually scrolled up.
func (m *PreviewModel) SetContent(content string) {
	m.content = content
	if m.viewport.Width > 0 {
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if ansi.StringWidth(line) > m.viewport.Width {
				lines[i] = ansi.Truncate(line, m.viewport.Width, "")
			}
		}
		content = strings.Join(lines, "\n")
	}
	// For live captures, auto-scroll to the bottom so the user sees the
	// latest output. This matches terminal behavior.
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

// SetSession updates the header metadata.
func (m *PreviewModel) SetSession(s *session.Session) {
	m.session = s
}

// SetMetrics updates the session metrics displayed in the header.
func (m *PreviewModel) SetMetrics(metrics *session.SessionMetrics) {
	m.metrics = metrics
}

// SetPassthrough sets whether the preview is in passthrough mode.
func (m *PreviewModel) SetPassthrough(on bool) {
	m.passthrough = on
}

// SetSize updates the preview panel dimensions. No border on the preview --
// the separator is the sidebar's right border.
func (m *PreviewModel) SetSize(w, h int) {
	m.width = w
	m.height = h

	// No border on preview, just header (2 lines: text + border-bottom) and
	// 1 char left padding for content.
	contentHeight := h - 2
	if contentHeight < 1 {
		contentHeight = 1
	}
	contentWidth := w - 2 // left/right padding
	if contentWidth < 1 {
		contentWidth = 1
	}
	m.viewport.Width = contentWidth
	m.viewport.Height = contentHeight

	// Re-wrap existing content to the new width so lines don't overflow.
	if m.content != "" {
		m.viewport.SetContent(m.content)
	}
}

// ScrollUp scrolls the viewport up by n lines.
func (m *PreviewModel) ScrollUp(n int) {
	m.viewport.SetYOffset(m.viewport.YOffset - n)
}

// ScrollDown scrolls the viewport down by n lines.
func (m *PreviewModel) ScrollDown(n int) {
	m.viewport.SetYOffset(m.viewport.YOffset + n)
}

// HalfViewHeight returns half the viewport height for half-page scrolling.
func (m *PreviewModel) HalfViewHeight() int {
	h := m.viewport.Height / 2
	if h < 1 {
		h = 1
	}
	return h
}

// Init satisfies the tea.Model interface.
func (m PreviewModel) Init() tea.Cmd {
	return nil
}

// Update handles viewport scrolling messages.
func (m PreviewModel) Update(msg tea.Msg) (PreviewModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View renders the preview panel with header and content. No border is rendered
// here -- the sidebar's right border serves as the separator.
func (m PreviewModel) View() string {
	header := m.renderHeader()
	body := m.viewport.View()

	inner := lipgloss.JoinVertical(lipgloss.Left, header, body)

	// No border on the preview panel. Just render with padding.
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		PaddingLeft(1).
		Render(inner)
}

func (m PreviewModel) renderHeader() string {
	sep := styles.PreviewSep.Render(" \u2022 ")
	maxWidth := m.width - 3 // account for left padding
	if maxWidth < 10 {
		maxWidth = 10
	}

	if m.session == nil {
		return styles.PreviewHeader.Width(maxWidth).Render("No session selected")
	}

	s := m.session

	projectName := s.ProjectName
	if projectName == "" {
		projectName = "unknown"
	}

	var leftParts []string

	// Project name in blue bold.
	leftParts = append(leftParts, lipgloss.NewStyle().Foreground(styles.ColorBlue).Bold(true).Render(projectName))

	// Git branch in green.
	if s.GitBranch != "" {
		leftParts = append(leftParts, styles.PreviewHeaderBranch.Render(s.GitBranch))
	}

	// Status text badge (colored).
	var statusBadge string
	switch s.Status {
	case session.StatusActive:
		statusBadge = styles.StatusBadgeActive.Render("ACTIVE")
	case session.StatusWaiting:
		statusBadge = styles.StatusBadgeWaiting.Render("WAITING")
	case session.StatusIdle:
		statusBadge = styles.StatusBadgeIdle.Render("IDLE")
	default:
		statusBadge = styles.StatusBadgeIdle.Render("CLOSED")
	}
	leftParts = append(leftParts, statusBadge)

	// Tmux target in gray (e.g. "infra:1.2").
	if s.TmuxTarget != "" {
		leftParts = append(leftParts, styles.PreviewHeaderLabel.Render(s.TmuxTarget))
	}

	leftLine := strings.Join(leftParts, sep)

	// Right side: uptime, message count, CPU, MEM, and relative time.
	var rightParts []string

	// Uptime (from metrics).
	if m.metrics != nil && !m.metrics.StartTime.IsZero() {
		uptime := time.Since(m.metrics.StartTime)
		uptimeStr := formatUptime(uptime)
		rightParts = append(rightParts, styles.PreviewHeaderLabel.Render("\u23f1 ")+styles.PreviewHeaderValue.Render(uptimeStr))
	}

	// Message count (from metrics).
	if m.metrics != nil && m.metrics.MessageCount > 0 {
		rightParts = append(rightParts, styles.PreviewHeaderLabel.Render("msgs ")+styles.PreviewHeaderValue.Render(fmt.Sprintf("%d", m.metrics.MessageCount)))
	}

	rightParts = append(rightParts, styles.PreviewHeaderValue.Render(fmt.Sprintf("CPU %.1f%%", s.CPU)))
	rightParts = append(rightParts, styles.PreviewHeaderValue.Render(fmt.Sprintf("MEM %.0fMB", s.Memory)))

	if !s.LastActivity.IsZero() {
		relTime := relativeTime(time.Since(s.LastActivity))
		rightParts = append(rightParts, styles.PreviewHeaderValue.Render(relTime))
	}

	rightLine := strings.Join(rightParts, sep)

	leftWidth := lipgloss.Width(leftLine)
	rightWidth := lipgloss.Width(rightLine)
	gap := maxWidth - leftWidth - rightWidth
	if gap < 1 {
		gap = 1
	}

	headerLine := leftLine + strings.Repeat(" ", gap) + rightLine

	// In passthrough mode, use a blue border on the header to visually
	// indicate the pane is focused.
	headerStyle := styles.PreviewHeader
	if m.passthrough {
		headerStyle = styles.PreviewHeaderFocused
	}
	return headerStyle.Width(maxWidth).Render(headerLine)
}

// SetGroupSummary renders a summary view for a tmux session group.
func (m *PreviewModel) SetGroupSummary(groupName string, sessions []*session.Session) {
	m.session = nil

	var active, waiting, idle int
	models := map[string]int{}
	projects := map[string]int{}
	var totalCPU float64
	var totalMem float64
	var newest time.Time

	for _, s := range sessions {
		switch s.Status {
		case session.StatusActive:
			active++
		case session.StatusWaiting:
			waiting++
		case session.StatusIdle:
			idle++
		}
		if s.Model != "" {
			models[s.Model]++
		}
		if s.ProjectName != "" {
			projects[s.ProjectName]++
		}
		totalCPU += s.CPU
		totalMem += s.Memory
		if s.LastActivity.After(newest) {
			newest = s.LastActivity
		}
	}

	label := lipgloss.NewStyle().Foreground(styles.ColorGray)
	value := lipgloss.NewStyle().Foreground(styles.ColorCyan)
	sep := lipgloss.NewStyle().Foreground(styles.ColorGray).Render(strings.Repeat("\u2500", 35))

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + lipgloss.NewStyle().Foreground(styles.ColorBlue).Bold(true).Render(groupName) + "\n")
	b.WriteString("  " + sep + "\n\n")

	// Status breakdown.
	b.WriteString("  " + label.Render("Sessions  ") + value.Render(fmt.Sprintf("%d", len(sessions))))
	if active > 0 {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(styles.ColorGreen).Render(fmt.Sprintf("%d active", active)))
	}
	if waiting > 0 {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(styles.ColorAmber).Render(fmt.Sprintf("%d waiting", waiting)))
	}
	if idle > 0 {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(styles.ColorGray).Render(fmt.Sprintf("%d idle", idle)))
	}
	b.WriteString("\n")

	// Resources (if any active).
	if active+waiting > 0 {
		b.WriteString("  " + label.Render("Resources ") +
			value.Render(fmt.Sprintf("CPU %.1f%%  MEM %.0fMB", totalCPU, totalMem)) + "\n")
	}

	// Last activity.
	if !newest.IsZero() {
		b.WriteString("  " + label.Render("Activity  ") +
			value.Render(relativeTime(time.Since(newest))+" ago") + "\n")
	}

	b.WriteString("\n")

	// Models used.
	if len(models) > 0 {
		b.WriteString("  " + label.Render("Models") + "\n")
		for model, count := range models {
			b.WriteString(fmt.Sprintf("    %s %s\n",
				value.Render(fmt.Sprintf("%d", count)),
				label.Render(model)))
		}
		b.WriteString("\n")
	}

	// Projects in this session.
	if len(projects) > 0 {
		b.WriteString("  " + label.Render("Projects") + "\n")
		for project, count := range projects {
			b.WriteString(fmt.Sprintf("    %s %s\n",
				value.Render(fmt.Sprintf("%d", count)),
				label.Render(project)))
		}
		b.WriteString("\n")
	}

	b.WriteString("  " + lipgloss.NewStyle().Foreground(styles.ColorGray).Render("Select a session to preview") + "\n")

	m.content = b.String()
	m.viewport.SetContent(m.content)
}

// formatUptime formats a duration into a compact human-readable string.
func formatUptime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, mins)
}

func (m PreviewModel) statusBadge(status session.SessionStatus) string {
	fg, icon := statusIconProps(status, 0)
	if icon == " " {
		return ""
	}
	return lipgloss.NewStyle().Foreground(fg).Render(fmt.Sprintf("%s %s", icon, status.String()))
}
