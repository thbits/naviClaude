package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/styles"
)

// StatusBarModel renders the bottom bar with contextual keybinding hints.
type StatusBarModel struct {
	width     int
	mode      string
	version   string
	listHints []statusHint // configurable list-mode hints
	errText   string
}

type statusHint struct {
	key  string
	desc string
}

// NewStatusBar creates a StatusBarModel with the given width and version string.
func NewStatusBar(width int, version string) StatusBarModel {
	return StatusBarModel{
		width:   width,
		mode:    "list",
		version: version,
	}
}

// StatusHintInput is the public type for setting status bar hints from outside the package.
type StatusHintInput struct {
	Key  string
	Desc string
}

// SetKeyHints updates the list-mode hints from the config-derived KeyMap.
func (m *StatusBarModel) SetKeyHints(hints []StatusHintInput) {
	m.listHints = make([]statusHint, len(hints))
	for i, h := range hints {
		m.listHints[i] = statusHint{key: h.Key, desc: h.Desc}
	}
}

// SetMode sets the current mode which determines which hints are displayed.
func (m *StatusBarModel) SetMode(mode string) {
	m.mode = mode
}

// SetSize updates the status bar width.
func (m *StatusBarModel) SetSize(w int) {
	m.width = w
}

// SetError shows a transient error message in the status bar.
func (m *StatusBarModel) SetError(msg string) {
	m.errText = msg
}

// ClearError removes the transient error message.
func (m *StatusBarModel) ClearError() {
	m.errText = ""
}

// Init satisfies the tea.Model interface.
func (m StatusBarModel) Init() tea.Cmd {
	return nil
}

// Update is a no-op; the status bar does not handle input.
func (m StatusBarModel) Update(msg tea.Msg) (StatusBarModel, tea.Cmd) {
	return m, nil
}

// View renders the status bar.
func (m StatusBarModel) View() string {
	if m.errText != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e")).Bold(true)
		bar := errStyle.Render(" " + m.errText)
		return styles.StatusBar.Width(m.width).Render(bar)
	}

	hints := m.hintsForMode()

	var parts []string
	sep := styles.StatusBarSep.Render(" \u2022 ")

	for i, h := range hints {
		part := styles.StatusBarKey.Render(h.key) + " " + styles.StatusBarDesc.Render(h.desc)
		parts = append(parts, part)
		if i < len(hints)-1 {
			parts = append(parts, sep)
		}
	}

	left := strings.Join(parts, "")
	right := styles.StatusBarVersion.Render(m.version)

	// Compute gap between left hints and right-aligned version.
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	gap := m.width - leftWidth - rightWidth
	if gap < 1 {
		gap = 1
	}

	bar := left + strings.Repeat(" ", gap) + right

	return styles.StatusBar.Width(m.width).Render(bar)
}

func (m StatusBarModel) hintsForMode() []statusHint {
	switch m.mode {
	case "passthrough":
		return []statusHint{
			{"Tab", "exit"},
			{"Ctrl+]", "exit"},
			{"Ctrl+f", "jump"},
		}
	case "search":
		return []statusHint{
			{"Esc", "cancel"},
			{"Enter", "select"},
		}
	case "detail":
		return []statusHint{{"any key", "close"}}
	case "stats":
		return []statusHint{{"Tab", "cycle filter"}, {"any key", "close"}}
	case "themepicker":
		return []statusHint{{"j/k", "navigate"}, {"Enter", "apply"}, {"Esc", "cancel"}}
	case "contextmenu":
		return []statusHint{{"j/k", "navigate"}, {"Enter", "select"}, {"Esc", "cancel"}}
	default: // "list"
		if len(m.listHints) > 0 {
			return m.listHints
		}
		return []statusHint{
			{"Enter", "focus"},
			{"f", "jump"},
			{"/", "search"},
			{"n", "new"},
			{"K", "kill"},
			{"?", "help"},
		}
	}
}
