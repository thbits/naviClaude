package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
			{"Tab/Shift+Tab", "Cycle panes"},
			{"Ctrl+U/Ctrl+D", "Scroll preview"},
			{"/", "Search"},
			{"Enter", "Focus / resume closed"},
			{"f", "Jump / resume closed, then jump"},
			{"n", "New session"},
			{"N", "New tmux session"},
			{"r", "Rename session"},
			{"K", "Kill session"},
			{"F", "Changed files panel"},
			{"d", "Detail"},
			{"s", "Stats"},
			{"T", "Theme picker"},
			{"?", "Help"},
			{"q", "Quit"},
		}
	}

	searchBindings := []helpBinding{
		{"Enter", "Focus / resume closed"},
		{"Ctrl+f", "Jump / resume + jump"},
		{"PgUp/PgDn", "Scroll preview"},
		{"Esc", "Cancel search"},
	}

	passthroughBindings := []helpBinding{
		{"Shift+Tab", "Back to previous pane"},
		{"Ctrl+]", "Back to list"},
		{"Ctrl+f", "Jump to pane"},
	}

	changedFilesBindings := []helpBinding{
		{"j/k", "Navigate files"},
		{"Enter/e", "Open in $EDITOR"},
		{"Esc", "Back to list"},
	}

	listCol := append([]string{styles.HelpTitle.Render("List Mode")}, renderBindings(listBindings)...)
	otherCol := append([]string{styles.HelpTitle.Render("Passthrough Mode")}, renderBindings(passthroughBindings)...)
	otherCol = append(otherCol, "", styles.HelpTitle.Render("Search"))
	otherCol = append(otherCol, renderBindings(searchBindings)...)
	otherCol = append(otherCol, "", styles.HelpTitle.Render("Changed Files"))
	otherCol = append(otherCol, renderBindings(changedFilesBindings)...)

	footer := styles.HelpDesc.Render("Press any key to close")
	render := func(body string) string {
		return styles.HelpBorder.Render(strings.Join([]string{title, "", body, "", footer}, "\n"))
	}

	stacked := render(strings.Join(listCol, "\n") + "\n\n" + strings.Join(otherCol, "\n"))
	if m.height <= 0 || lipgloss.Height(stacked) <= m.height {
		return stacked
	}
	// Too tall for the terminal: put the sections side by side when that fits.
	side := render(lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(listCol, "\n"), "    ", strings.Join(otherCol, "\n")))
	if lipgloss.Width(side) <= m.width {
		return side
	}
	return stacked
}

func renderBindings(bindings []helpBinding) []string {
	// Find the longest key for alignment (display-width aware, shared helper).
	keys := make([]string, len(bindings))
	for i, b := range bindings {
		keys[i] = b.key
	}
	maxKeyLen := labelColumnWidth(keys)

	var lines []string
	for _, b := range bindings {
		padding := strings.Repeat(" ", alignedLabelPad(b.key, maxKeyLen, 2))
		line := styles.HelpKey.Render(b.key) + padding + styles.HelpDesc.Render(b.desc)
		lines = append(lines, line)
	}
	return lines
}
