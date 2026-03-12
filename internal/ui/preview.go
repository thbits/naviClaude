package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tomhalo/naviclaude/internal/session"
	"github.com/tomhalo/naviclaude/internal/styles"
)

// PreviewModel is the right panel that shows captured terminal content for the
// selected session.
type PreviewModel struct {
	content     string
	session     *session.Session
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
func (m *PreviewModel) SetContent(content string) {
	m.content = content
	m.viewport.SetContent(content)
}

// SetSession updates the header metadata.
func (m *PreviewModel) SetSession(s *session.Session) {
	m.session = s
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
	sep := styles.PreviewSep.Render(" | ")
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

	// Status badge (colored).
	leftParts = append(leftParts, m.statusBadge(s.Status))

	// Tmux target in gray (e.g. "infra:1.2").
	if s.TmuxTarget != "" {
		leftParts = append(leftParts, styles.PreviewHeaderLabel.Render(s.TmuxTarget))
	}

	leftLine := strings.Join(leftParts, sep)

	// Right side: CPU, MEM, and relative time.
	var rightParts []string
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

func (m PreviewModel) statusBadge(status session.SessionStatus) string {
	fg, icon := statusIconProps(status)
	if icon == " " {
		return ""
	}
	return lipgloss.NewStyle().Foreground(fg).Render(fmt.Sprintf("%s %s", icon, status.String()))
}
