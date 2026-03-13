package ui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/styles"
)

// NameInputModel provides a single-line text input for naming a new tmux session.
type NameInputModel struct {
	input  textinput.Model
	active bool
	width  int
}

// NewNameInput creates a NameInputModel.
func NewNameInput() NameInputModel {
	ti := textinput.New()
	ti.Prompt = "Session name: "
	ti.PromptStyle = styles.SearchPrompt
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.ColorFg)
	ti.Placeholder = "enter name or press Enter for default"
	ti.CharLimit = 64
	return NameInputModel{input: ti}
}

// Activate shows the input and focuses it.
func (m *NameInputModel) Activate() tea.Cmd {
	m.active = true
	m.input.SetValue("")
	m.input.Focus()
	return textinput.Blink
}

// Deactivate clears and hides the input.
func (m *NameInputModel) Deactivate() {
	m.active = false
	m.input.SetValue("")
	m.input.Blur()
}

// IsActive returns whether the input is visible.
func (m *NameInputModel) IsActive() bool {
	return m.active
}

// Value returns the current text value.
func (m *NameInputModel) Value() string {
	return m.input.Value()
}

// SetSize updates the width.
func (m *NameInputModel) SetSize(w int) {
	m.width = w
	m.input.Width = w - 20 // account for prompt and padding
}

// Update handles text input events.
func (m NameInputModel) Update(msg tea.Msg) (NameInputModel, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View renders the input bar.
func (m NameInputModel) View() string {
	if !m.active {
		return ""
	}
	inputView := m.input.View()
	return styles.SearchInput.Width(m.width - 4).Render(inputView)
}
