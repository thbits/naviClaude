package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/styles"
)

// handleThemePickerKey handles input in the theme picker overlay.
func (m Model) handleThemePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Confirm: keep the current theme, save to config.
		selected := m.themePicker.SelectedKey()
		// With theme_auto on, the pick belongs to the appearance it was made in.
		field := "theme"
		switch {
		case !m.cfg.ThemeAuto:
			m.cfg.Theme = selected
		case m.osDark:
			m.cfg.ThemeDark, field = selected, "theme_dark"
		default:
			m.cfg.ThemeLight, field = selected, "theme_light"
		}
		m.setTheme(selected)
		if err := m.writeConfig(); err != nil {
			m.statusbar.SetError("theme saved in session only (config write failed: " + err.Error() + ")")
		} else if m.cfg.ThemeAuto {
			m.statusbar.SetNotice(field + " set to " + selected)
		}
		m.themePicker.Hide()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m, nil

	case "esc":
		// Cancel: restore the original theme.
		m.setTheme(m.themePicker.OriginalKey())
		m.themePicker.Hide()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m, nil

	default:
		// Navigation -- apply theme live.
		var changed bool
		var newKey string
		m.themePicker, newKey, changed = m.themePicker.Update(msg)
		if changed {
			styles.ApplyTheme(styles.Named(newKey))
		}
		return m, nil
	}
}
