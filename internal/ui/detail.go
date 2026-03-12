package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tomhalo/naviclaude/internal/session"
	"github.com/tomhalo/naviclaude/internal/styles"
)

// DetailModel renders a centered popup with session metadata.
type DetailModel struct {
	visible      bool
	session      *session.Session
	messageCount int
	startTime    time.Time
	width        int
	height       int
}

// NewDetail creates a DetailModel.
func NewDetail() DetailModel {
	return DetailModel{}
}

// Show displays the detail popup for the given session.
func (m *DetailModel) Show(s *session.Session) {
	m.visible = true
	m.session = s
	m.messageCount = 0
	m.startTime = time.Time{}
}

// Hide hides the detail popup.
func (m *DetailModel) Hide() {
	m.visible = false
}

// IsVisible returns whether the overlay is visible.
func (m *DetailModel) IsVisible() bool {
	return m.visible
}

// SetSize updates the overlay container dimensions.
func (m *DetailModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetData sets the message count and start time loaded from the session file.
func (m *DetailModel) SetData(count int, start time.Time) {
	m.messageCount = count
	m.startTime = start
}

// View renders the centered detail overlay.
func (m DetailModel) View() string {
	if !m.visible || m.session == nil {
		return ""
	}

	s := m.session

	title := styles.DetailTitle.Render(s.ProjectName)

	rows := []detailRow{
		{"CWD", s.CWD},
		{"Git Branch", s.GitBranch},
		{"Status", s.Status.String()},
		{"Session ID", s.ID},
		{"Tmux Target", s.TmuxTarget},
	}

	// Session file path.
	sessionFile := s.SessionFile
	if sessionFile == "" && s.ID != "" && s.CWD != "" {
		sessionFile = session.SessionFilePath(s.ID, s.CWD)
	}
	rows = append(rows, detailRow{"Session File", sessionFile})
	rows = append(rows, detailRow{"Slug", s.Slug})
	rows = append(rows, detailRow{"Model", s.Model})
	rows = append(rows, detailRow{"Version", s.Version})

	// Time-based fields.
	if !m.startTime.IsZero() {
		rows = append(rows, detailRow{"Start Time", m.startTime.Format("2006-01-02 15:04:05")})
		uptime := time.Since(m.startTime)
		rows = append(rows, detailRow{"Uptime", formatDuration(uptime)})
	}

	if m.messageCount > 0 {
		rows = append(rows, detailRow{"Messages", fmt.Sprintf("%d", m.messageCount)})
	}

	// CPU/Memory for active sessions.
	if s.Status != session.StatusClosed {
		rows = append(rows, detailRow{"CPU", fmt.Sprintf("%.1f%%", s.CPU)})
		rows = append(rows, detailRow{"Memory", fmt.Sprintf("%.0f MB", s.Memory)})
	}

	// Find longest label for alignment.
	maxLabel := 0
	for _, r := range rows {
		if len(r.label) > maxLabel {
			maxLabel = len(r.label)
		}
	}

	var lines []string
	lines = append(lines, title)
	lines = append(lines, "")

	for _, r := range rows {
		if r.value == "" {
			continue
		}
		padding := strings.Repeat(" ", maxLabel-len(r.label)+2)
		line := styles.DetailLabel.Render(r.label) + padding + styles.DetailValue.Render(r.value)
		lines = append(lines, line)
	}

	lines = append(lines, "")
	lines = append(lines, styles.HelpDesc.Render("Press any key to close"))

	content := strings.Join(lines, "\n")
	popup := styles.DetailBorder.Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
}

type detailRow struct {
	label string
	value string
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
}
