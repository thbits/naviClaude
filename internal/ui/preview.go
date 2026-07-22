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

// cacheExpiryThreshold is how long a live session can sit idle before Claude's
// prompt cache is assumed to have expired. Past this point, resuming the
// session re-processes its whole context as uncached input tokens, so the next
// message costs far more than a cached resume would.
const cacheExpiryThreshold = time.Hour

// PreviewModel is the right panel that shows captured terminal content for the
// selected session.
type PreviewModel struct {
	content      string
	session      *session.Session
	metrics      *session.SessionMetrics
	passthrough  bool
	userScrolled bool // true when user has scrolled away from bottom
	viewport     viewport.Model
	width        int
	height       int
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
	m.viewport.SetContent(m.truncateToWidth(content))
	// Only auto-scroll to bottom if the user hasn't scrolled away.
	// This lets users read history without being yanked back every 200ms.
	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
}

// truncateToWidth applies the per-line ANSI-aware width truncation used before
// any content is handed to the viewport (a safety net for stale scrollback;
// the tmux pane is resized to match so apps normally re-render at the correct
// width via SIGWINCH). Shared by SetContent and SetSize so a resize doesn't
// leave stale long lines overflowing the narrower viewport.
func (m *PreviewModel) truncateToWidth(content string) string {
	if m.viewport.Width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > m.viewport.Width {
			lines[i] = ansi.Truncate(line, m.viewport.Width, "")
		}
	}
	return strings.Join(lines, "\n")
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

	// Re-truncate existing content to the new width so stale long lines don't
	// overflow the narrower viewport (same width pass as SetContent).
	if m.content != "" {
		m.viewport.SetContent(m.truncateToWidth(m.content))
	}
}

// ScrollUp scrolls the viewport up by n lines and pauses auto-scroll.
func (m *PreviewModel) ScrollUp(n int) {
	m.viewport.SetYOffset(m.viewport.YOffset - n)
	m.userScrolled = true
}

// ScrollDown scrolls the viewport down by n lines.
// Resumes auto-scroll when the user reaches the bottom.
func (m *PreviewModel) ScrollDown(n int) {
	m.viewport.SetYOffset(m.viewport.YOffset + n)
	if m.atBottom() {
		m.userScrolled = false
	}
}

// atBottom reports whether the viewport is scrolled to the last line of the
// content. The total line count is strings.Count("\n")+1 (a string with k
// newlines has k+1 lines); using the raw count undercounts by one and resumes
// auto-follow a line early.
func (m *PreviewModel) atBottom() bool {
	totalLines := strings.Count(m.content, "\n") + 1
	return m.viewport.YOffset+m.viewport.Height >= totalLines
}

// ResetScroll clears the user scroll flag (e.g. on session change).
func (m *PreviewModel) ResetScroll() {
	m.userScrolled = false
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

	// Track user scroll state after viewport processes the event.
	switch msg.(type) {
	case tea.MouseMsg:
		m.userScrolled = !m.atBottom()
	}

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

	// Status label shared by both render paths.
	statusLabel := "CLOSED"
	switch s.Status {
	case session.StatusActive:
		statusLabel = "ACTIVE"
	case session.StatusWaiting:
		statusLabel = "WAITING"
	case session.StatusIdle:
		statusLabel = "IDLE"
	}

	// Right-side metrics as plain strings (styled per render path below).
	var rightPlain []string
	if m.metrics != nil && !m.metrics.StartTime.IsZero() {
		rightPlain = append(rightPlain, "⏱ "+formatUptime(time.Since(m.metrics.StartTime)))
	}
	if m.metrics != nil && m.metrics.MessageCount > 0 {
		rightPlain = append(rightPlain, fmt.Sprintf("msgs %d", m.metrics.MessageCount))
	}
	rightPlain = append(rightPlain, fmt.Sprintf("CPU %.1f%%", s.CPU))
	rightPlain = append(rightPlain, fmt.Sprintf("MEM %.0fMB", s.Memory))
	if !s.LastActivity.IsZero() {
		rightPlain = append(rightPlain, relativeTime(time.Since(s.LastActivity)))
	}

	const sepStr = " • "

	// The header must render on exactly one line (+ its bottom border = 2 rows):
	// the viewport height is sized as paneHeight-2 assuming a single-line header,
	// so a wrapped header would steal a row. Both header styles carry
	// PaddingLeft(1)+PaddingRight(1), so the text area is maxWidth-2; budget the
	// gap against that and truncate as a hard guard so overflow is cut, not wrapped.
	const headerPadding = 2
	contentWidth := maxWidth - headerPadding
	if contentWidth < 1 {
		contentWidth = 1
	}

	var headerLine string
	var headerStyle lipgloss.Style

	if m.passthrough {
		// --- Focused: two-tone band = blue identity chip + muted metrics band. ---
		// Every band segment (and its separators/fill) carries the muted
		// background so the fill tiles continuously with no ANSI-reset gaps,
		// mirroring the sidebar's selected-row rendering.
		bg := styles.ColorBgHover

		// Identity chip: blue reverse-video, with the branch folded in.
		chipText := " ▸ " + projectName
		if s.GitBranch != "" {
			chipText += sepStr + s.GitBranch
		}
		chipText += " "
		// Cap the identity chip so the metrics band always keeps room on
		// narrow panes; the final truncate below is the last-resort guard.
		if maxChip := contentWidth - 12; maxChip >= 8 && lipgloss.Width(chipText) > maxChip {
			chipText = truncateDisplay(chipText, maxChip)
		}
		chip := styles.PaneTitleActive.Render(chipText)
		bandWidth := contentWidth - lipgloss.Width(chip)
		if bandWidth < 1 {
			bandWidth = 1
		}

		bandSep := lipgloss.NewStyle().Foreground(styles.ColorGray).Background(bg).Render(sepStr)

		// Left of band: status (semantic color), cache warning, tmux target.
		statusFg := styles.ColorGray
		switch s.Status {
		case session.StatusActive:
			statusFg = styles.ColorGreen
		case session.StatusWaiting:
			statusFg = styles.ColorAmber
		}
		var bandLeft []string
		bandLeft = append(bandLeft, lipgloss.NewStyle().Foreground(statusFg).Background(bg).Bold(true).Render(statusLabel))
		if cacheExpired(s, time.Now()) {
			bandLeft = append(bandLeft, lipgloss.NewStyle().Foreground(styles.ColorRed).Background(bg).Bold(true).Render("cache expired"))
		}
		if s.TmuxTarget != "" {
			bandLeft = append(bandLeft, lipgloss.NewStyle().Foreground(styles.ColorGray).Background(bg).Render(s.TmuxTarget))
		}

		// Right of band: metrics, values on the muted bg.
		valStyle := lipgloss.NewStyle().Foreground(styles.ColorCyan).Background(bg)
		var bandRight []string
		for _, v := range rightPlain {
			bandRight = append(bandRight, valStyle.Render(v))
		}

		// Connect the muted band to the blue identity chip with a separator dot
		// so it reads "... branch • ACTIVE" instead of the name butting straight
		// against the status. The chip supplies the leading space; the seam adds
		// the dot on the muted background.
		seam := lipgloss.NewStyle().Foreground(styles.ColorGray).Background(bg).Render("• ")
		leftStr := seam + strings.Join(bandLeft, bandSep)
		rightStr := strings.Join(bandRight, bandSep)
		gap := bandWidth - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
		if gap < 1 {
			gap = 1
		}
		gapStr := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gap))
		bandInner := ansi.Truncate(leftStr+gapStr+rightStr, bandWidth, "")
		band := lipgloss.NewStyle().Background(bg).Width(bandWidth).Render(bandInner)

		// Hard single-line guard (mirrors the unfocused path): never let the
		// composed bar exceed the content width and wrap into a second row.
		headerLine = ansi.Truncate(chip+band, contentWidth, "")
		headerStyle = styles.PreviewHeaderFocused
	} else {
		// --- Unfocused: flat dim, matching the inactive SESSIONS / CHANGED
		// FILES titles. All fields render in DimText; the thin gray underline
		// carries the (lack of) focus. ---
		leftPlain := []string{projectName}
		if s.GitBranch != "" {
			leftPlain = append(leftPlain, s.GitBranch)
		}
		leftPlain = append(leftPlain, statusLabel)
		if cacheExpired(s, time.Now()) {
			leftPlain = append(leftPlain, "cache expired")
		}
		if s.TmuxTarget != "" {
			leftPlain = append(leftPlain, s.TmuxTarget)
		}
		leftStr := strings.Join(leftPlain, sepStr)
		rightStr := strings.Join(rightPlain, sepStr)
		gap := contentWidth - lipgloss.Width(leftStr) - lipgloss.Width(rightStr)
		if gap < 1 {
			gap = 1
		}
		headerLine = ansi.Truncate(leftStr+strings.Repeat(" ", gap)+rightStr, contentWidth, "")
		headerStyle = styles.PreviewHeader.Foreground(styles.ColorDimText)
	}

	return headerStyle.Width(maxWidth).Render(headerLine)
}

// cacheExpired reports whether a session has been idle long enough that
// Claude's prompt cache will have expired. Closed sessions warn too: resuming
// one re-sends the whole conversation, so a cold cache still means a costly
// re-process. Sessions with no known last-activity time never warn.
func cacheExpired(s *session.Session, now time.Time) bool {
	if s == nil || s.LastActivity.IsZero() {
		return false
	}
	return now.Sub(s.LastActivity) >= cacheExpiryThreshold
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
