package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/styles"
)

// ThemeEntry is a theme name + display label pair.
type ThemeEntry struct {
	Key  string // config key e.g. "catppuccin-mocha"
	Name string // display name e.g. "Catppuccin Mocha"
}

// ThemePickerModel is an overlay that lets the user select a theme live.
type ThemePickerModel struct {
	visible       bool
	themes        []ThemeEntry
	cursor        int
	activeKey     string // currently applied theme key
	originalKey   string // theme when picker was opened (for Esc restore)
	width, height int
}

// NewThemePicker creates a ThemePickerModel populated from styles.Themes.
func NewThemePicker(currentTheme string) ThemePickerModel {
	names := styles.ThemeNames() // sorted list of keys
	entries := make([]ThemeEntry, len(names))
	for i, k := range names {
		p := styles.Themes[k]
		entries[i] = ThemeEntry{Key: k, Name: p.Name}
	}
	m := ThemePickerModel{
		themes:      entries,
		activeKey:   currentTheme,
		originalKey: currentTheme,
	}
	// Position cursor on the current theme.
	for i, e := range entries {
		if e.Key == currentTheme {
			m.cursor = i
			break
		}
	}
	return m
}

// Show opens the picker, saving the current theme for potential restore.
func (m *ThemePickerModel) Show(currentTheme string) {
	m.visible = true
	m.activeKey = currentTheme
	m.originalKey = currentTheme
	// Move cursor to the current theme.
	for i, e := range m.themes {
		if e.Key == currentTheme {
			m.cursor = i
			break
		}
	}
}

// Hide closes the picker.
func (m *ThemePickerModel) Hide() { m.visible = false }

// IsVisible returns whether the picker is open.
func (m *ThemePickerModel) IsVisible() bool { return m.visible }

// SelectedKey returns the currently highlighted theme key.
func (m *ThemePickerModel) SelectedKey() string {
	if m.cursor < 0 || m.cursor >= len(m.themes) {
		return m.activeKey
	}
	return m.themes[m.cursor].Key
}

// OriginalKey returns the theme that was active when the picker was opened.
func (m *ThemePickerModel) OriginalKey() string { return m.originalKey }

// SetSize updates the popup container dimensions.
func (m *ThemePickerModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Init satisfies tea.Model.
func (m ThemePickerModel) Init() tea.Cmd { return nil }

// Update handles j/k navigation. Enter and Esc are handled by the app.
// Returns the newly highlighted theme key so the app can apply it live,
// and a bool indicating whether the theme changed.
func (m ThemePickerModel) Update(msg tea.Msg) (ThemePickerModel, string, bool) {
	if !m.visible {
		return m, "", false
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.themes)-1 {
				m.cursor++
				return m, m.themes[m.cursor].Key, true
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
				return m, m.themes[m.cursor].Key, true
			}
		}
	}
	return m, "", false
}

// swatch renders a row of colored blocks for a palette so you can see
// the accent colors before applying the theme.
func swatch(p styles.Palette) string {
	accents := []lipgloss.Color{p.Blue, p.Green, p.Amber, p.Red, p.Purple, p.Cyan}
	var b strings.Builder
	for _, c := range accents {
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render("█"))
	}
	return b.String()
}

// View renders the theme picker popup (without placement -- caller uses PlaceOverlay).
func (m ThemePickerModel) View() string {
	if !m.visible {
		return ""
	}

	title := lipgloss.NewStyle().Foreground(styles.ColorPurple).Bold(true).Render("Themes")

	var rows []string
	rows = append(rows, title)
	rows = append(rows, "")

	for i, entry := range m.themes {
		isCursor := i == m.cursor
		palette := styles.Themes[entry.Key]
		sw := swatch(palette)

		bullet := "  "
		if entry.Key == m.originalKey {
			bullet = lipgloss.NewStyle().Foreground(styles.ColorAmber).Render("● ")
		}

		label := fmt.Sprintf("%s%-22s %s", bullet, entry.Name, sw)

		var row string
		if isCursor {
			row = lipgloss.NewStyle().
				Foreground(styles.ColorBlue).
				Background(styles.ColorSelection).
				Bold(true).
				PaddingLeft(1).
				PaddingRight(1).
				Render(label)
		} else {
			row = lipgloss.NewStyle().
				Foreground(styles.ColorFg).
				PaddingLeft(1).
				PaddingRight(1).
				Render(label)
		}
		rows = append(rows, row)
	}

	rows = append(rows, "")
	footer := lipgloss.NewStyle().Foreground(styles.ColorGray).Render("Enter apply  Esc cancel")
	rows = append(rows, footer)

	content := strings.Join(rows, "\n")

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorPurple).
		Background(styles.ColorBgPanel).
		Padding(1, 2)

	return border.Render(content)
}
