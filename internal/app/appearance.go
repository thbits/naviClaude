package app

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/styles"
)

// appearanceInterval is how often the OS appearance is re-read with theme_auto on.
const appearanceInterval = 5 * time.Second

// defaultLightTheme stands in for an empty theme_light.
const defaultLightTheme = "catppuccin-latte"

// appearanceMsg carries a fresh reading of the OS appearance.
type appearanceMsg struct{ dark bool }

// systemDark reports whether the OS is in dark mode: macOS's AppleInterfaceStyle
// (unset in light mode, so the read fails), elsewhere the terminal background.
func systemDark() bool {
	if runtime.GOOS != "darwin" {
		return lipgloss.HasDarkBackground()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleInterfaceStyle").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Dark"
}

// themeFor is the theme key cfg asks for: theme, or with theme_auto on the one
// for the OS appearance (theme_dark falls back to theme).
func themeFor(cfg config.Config, dark bool) string {
	name := cfg.Theme
	if cfg.ThemeAuto {
		if !dark {
			name = cfg.ThemeLight
			if name == "" {
				name = defaultLightTheme
			}
		} else if cfg.ThemeDark != "" {
			name = cfg.ThemeDark
		}
	}
	return styles.Key(name)
}

// currentTheme is the applied theme's key, or the configured one before any
// was applied through setTheme.
func (m Model) currentTheme() string {
	if m.themeName != "" {
		return m.themeName
	}
	return themeFor(m.cfg, m.osDark)
}

// setTheme applies the theme called name live.
func (m *Model) setTheme(name string) {
	m.themeName = styles.Key(name)
	styles.ApplyTheme(styles.Named(m.themeName))
}

// appearanceCmd re-reads the OS appearance off the UI goroutine when a check is
// due. Only macOS is polled: the terminal can't be queried once the UI owns it.
func (m *Model) appearanceCmd() tea.Cmd {
	detect := m.detectDark
	if detect == nil {
		if runtime.GOOS != "darwin" {
			return nil
		}
		detect = systemDark
	}
	now := m.clock()
	if !m.cfg.ThemeAuto || m.appearanceInFlight || now.Sub(m.appearanceAt) < appearanceInterval {
		return nil
	}
	m.appearanceInFlight = true
	m.appearanceAt = now
	return func() tea.Msg { return appearanceMsg{dark: detect()} }
}

// followAppearance switches to the theme for the OS appearance. An open theme
// picker keeps previewing its own choice, but its save must target the current mode.
func (m *Model) followAppearance(dark bool) {
	if !m.cfg.ThemeAuto {
		return
	}
	m.osDark = dark
	if m.themePicker.IsVisible() {
		return
	}
	if name := themeFor(m.cfg, dark); name != m.currentTheme() {
		m.setTheme(name)
	}
}
