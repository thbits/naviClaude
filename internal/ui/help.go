package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/styles"
)

// HelpModel renders a centered overlay popup with keybinding reference.
type HelpModel struct {
	visible      bool
	width        int
	height       int
	listBindings []helpBinding // configurable from KeyMap
}

type helpBinding struct {
	key  string
	desc string
}

// NewHelp creates a HelpModel.
func NewHelp() HelpModel {
	return HelpModel{}
}

// HelpBindingInput is the public type for setting key bindings from outside the package.
type HelpBindingInput struct {
	Key  string
	Desc string
}

// SetKeyBindings updates the list-mode bindings from the config-derived KeyMap.
func (m *HelpModel) SetKeyBindings(bindings []HelpBindingInput) {
	m.listBindings = make([]helpBinding, len(bindings))
	for i, b := range bindings {
		m.listBindings[i] = helpBinding{key: b.Key, desc: b.Desc}
	}
}

// Show makes the help overlay visible.
func (m *HelpModel) Show() {
	m.visible = true
}

// Hide hides the help overlay.
func (m *HelpModel) Hide() {
	m.visible = false
}

// Toggle toggles the help overlay visibility.
func (m *HelpModel) Toggle() {
	m.visible = !m.visible
}

// IsVisible returns whether the overlay is visible.
func (m *HelpModel) IsVisible() bool {
	return m.visible
}

// SetSize updates the overlay container dimensions.
func (m *HelpModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Init satisfies the tea.Model interface.
func (m HelpModel) Init() tea.Cmd {
	return nil
}

// Update handles any key to close the overlay.
func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	switch msg.(type) {
	case tea.KeyMsg:
		m.visible = false
	}
	return m, nil
}

// View renders the centered help overlay.
func (m HelpModel) View() string {
	if !m.visible {
		return ""
	}

	title := styles.HelpTitle.Render("Help - Keybindings")

	listBindings := m.listBindings
	if len(listBindings) == 0 {
		// Fallback defaults if SetKeyBindings was never called.
		listBindings = []helpBinding{
			{"j/k", "Navigate sessions"},
			{"Enter/Tab", "Focus (passthrough)"},
			{"f", "Jump to pane"},
			{"/", "Search"},
			{"n", "New session"},
			{"K", "Kill session"},
			{"d", "Detail"},
			{"s", "Stats"},
			{"?", "Help"},
			{"q", "Quit"},
		}
	}

	passthroughBindings := []helpBinding{
		{"Tab/Ctrl+]", "Exit passthrough"},
		{"Ctrl+f", "Jump to pane"},
	}

	var lines []string
	lines = append(lines, title)
	lines = append(lines, "")
	lines = append(lines, styles.HelpTitle.Render("List Mode"))
	lines = append(lines, renderBindings(listBindings)...)
	lines = append(lines, "")
	lines = append(lines, styles.HelpTitle.Render("Passthrough Mode"))
	lines = append(lines, renderBindings(passthroughBindings)...)
	lines = append(lines, "")
	lines = append(lines, styles.HelpDesc.Render("Press any key to close"))

	content := strings.Join(lines, "\n")

	return styles.HelpBorder.Render(content)
}

func renderBindings(bindings []helpBinding) []string {
	// Find the longest key for alignment.
	maxKeyLen := 0
	for _, b := range bindings {
		if len(b.key) > maxKeyLen {
			maxKeyLen = len(b.key)
		}
	}

	var lines []string
	for _, b := range bindings {
		padding := strings.Repeat(" ", maxKeyLen-len(b.key)+2)
		line := styles.HelpKey.Render(b.key) + padding + styles.HelpDesc.Render(b.desc)
		lines = append(lines, line)
	}
	return lines
}
