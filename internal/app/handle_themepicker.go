package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/styles"
)

// handleThemePickerKey handles input in the theme picker overlay.
func (m Model) handleThemePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Confirm: keep the current theme, save to config.
		selected := m.themePicker.SelectedKey()
		m.cfg.Theme = selected
		if err := config.Save(m.cfg, ""); err != nil {
			m.statusbar.SetError("theme saved in session only (config write failed: " + err.Error() + ")")
		}
		m.themePicker.Hide()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m, nil

	case "esc":
		// Cancel: restore the original theme.
		original := m.themePicker.OriginalKey()
		styles.ApplyTheme(styles.Named(original))
		m.cfg.Theme = original
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
