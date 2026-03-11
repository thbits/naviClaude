package ui

import (
	"fmt"
	"strings"

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
	vp := viewport.New(width, height-2) // subtract 2 for border+header
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

// SetSize updates the preview panel dimensions.
func (m *PreviewModel) SetSize(w, h int) {
	m.width = w
	m.height = h

	// Account for border (2 lines) and header (1 line).
	contentHeight := h - 3
	if contentHeight < 1 {
		contentHeight = 1
	}
	contentWidth := w - 2
	if contentWidth < 1 {
		contentWidth = 1
	}
	m.viewport.Width = contentWidth
	m.viewport.Height = contentHeight
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

// View renders the preview panel with border, header, and content.
func (m PreviewModel) View() string {
	header := m.renderHeader()
	body := m.viewport.View()

	inner := lipgloss.JoinVertical(lipgloss.Left, header, body)

	borderStyle := styles.PreviewBorderUnfocused
	if m.passthrough {
		borderStyle = styles.PreviewBorderFocused
	}

	return borderStyle.
		Width(m.width - 2).   // subtract border width
		Height(m.height - 2). // subtract border height
		Render(inner)
}

func (m PreviewModel) renderHeader() string {
	if m.session == nil {
		return styles.PreviewHeader.Width(m.width - 2).Render("No session selected")
	}

	s := m.session

	// Project name.
	projectName := s.ProjectName
	if projectName == "" {
		projectName = "unknown"
	}

	parts := []string{
		styles.PreviewHeader.Render(projectName),
	}

	// Git branch.
	if s.GitBranch != "" {
		parts = append(parts, styles.PreviewHeaderBranch.Render(s.GitBranch))
	}

	// Status badge.
	parts = append(parts, m.statusBadge(s.Status))

	// Model.
	if s.Model != "" {
		parts = append(parts, styles.PreviewHeaderValue.Render(s.Model))
	}

	// Tmux target.
	if s.TmuxTarget != "" {
		parts = append(parts, styles.PreviewHeaderLabel.Render(s.TmuxTarget))
	}

	// Passthrough badge.
	if m.passthrough {
		parts = append(parts, styles.PreviewPassthroughBadge.Render("PASSTHROUGH"))
	}

	headerLine := strings.Join(parts, styles.PreviewHeaderLabel.Render(" | "))

	// Truncate to width if needed.
	maxWidth := m.width - 2
	if maxWidth < 0 {
		maxWidth = 0
	}
	return lipgloss.NewStyle().
		MaxWidth(maxWidth).
		Width(maxWidth).
		Background(styles.ColorDim).
		Render(headerLine)
}

func (m PreviewModel) statusBadge(status session.SessionStatus) string {
	switch status {
	case session.StatusActive:
		return styles.StatusIconActive.Render(fmt.Sprintf("\u25cf %s", status.String()))
	case session.StatusWaiting:
		return styles.StatusIconWaiting.Render(fmt.Sprintf("\u25ce %s", status.String()))
	case session.StatusIdle:
		return styles.StatusIconIdle.Render(fmt.Sprintf("\u25cb %s", status.String()))
	case session.StatusClosed:
		return styles.StatusIconClosed.Render(fmt.Sprintf("\u25cc %s", status.String()))
	default:
		return ""
	}
}
