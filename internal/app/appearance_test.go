package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/styles"
	"github.com/thbits/naviClaude/internal/ui"
)

func TestThemeForFollowsAppearanceOnlyWhenAuto(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Theme = "nord"
	for _, dark := range []bool{true, false} {
		if got := themeFor(cfg, dark); got != "nord" {
			t.Errorf("auto off, dark=%v: theme = %q, want nord", dark, got)
		}
	}

	cfg.ThemeAuto = true
	if got := themeFor(cfg, false); got != "catppuccin-latte" {
		t.Errorf("light: theme = %q, want the default theme_light", got)
	}
	if got := themeFor(cfg, true); got != "nord" {
		t.Errorf("dark with theme_dark unset: theme = %q, want theme (nord)", got)
	}
	cfg.ThemeDark, cfg.ThemeLight = "dracula", "Rose Pine Dawn" // display names resolve too
	if got := themeFor(cfg, true); got != "dracula" {
		t.Errorf("dark: theme = %q, want dracula", got)
	}
	if got := themeFor(cfg, false); got != "rose-pine-dawn" {
		t.Errorf("light: theme = %q, want rose-pine-dawn", got)
	}
	cfg.ThemeLight = ""
	if got := themeFor(cfg, false); got != defaultLightTheme {
		t.Errorf("empty theme_light: theme = %q, want %q", got, defaultLightTheme)
	}
}

// autoModel follows the OS appearance, reading it from *dark at clock *now.
func autoModel(t *testing.T, dark *bool, now *time.Time) *Model {
	t.Helper()
	t.Cleanup(func() { styles.ApplyTheme(styles.Named("tokyo-night")) })
	m := statusModel(t, statusSessions())
	m.cfg = config.DefaultConfig()
	m.cfg.ThemeAuto = true
	m.cfg.Theme = "nord"
	m.themePicker = ui.NewThemePicker("nord")
	m.detectDark = func() bool { return *dark }
	m.now = func() time.Time { return *now }
	m.osDark = true
	m.setTheme("nord")
	m.appearanceAt = *now
	return m
}

func TestAppearancePollIsThrottledAndSwitchesTheme(t *testing.T) {
	dark, now := true, time.Now()
	m := autoModel(t, &dark, &now)

	if cmd := m.appearanceCmd(); cmd != nil {
		t.Fatal("a check ran before the interval passed")
	}
	now = now.Add(appearanceInterval)
	dark = false
	cmd := m.appearanceCmd()
	if cmd == nil {
		t.Fatal("no check after the interval")
	}
	if again := m.appearanceCmd(); again != nil {
		t.Error("a second check started while one was in flight")
	}

	send(t, m, cmd())
	if m.themeName != "catppuccin-latte" || styles.ColorBg != styles.Themes["catppuccin-latte"].Bg {
		t.Errorf("theme = %q (bg %s), want catppuccin-latte applied", m.themeName, styles.ColorBg)
	}
	if m.appearanceInFlight {
		t.Error("the in-flight guard was not released")
	}
	if m.cfg.Theme != "nord" {
		t.Errorf("following the OS rewrote theme to %q", m.cfg.Theme)
	}

	now = now.Add(appearanceInterval)
	dark = true
	send(t, m, m.appearanceCmd()())
	if m.themeName != "nord" {
		t.Errorf("theme = %q after the OS went dark, want nord", m.themeName)
	}
}

func TestAppearanceIgnoredWhenAutoOff(t *testing.T) {
	dark, now := false, time.Now()
	m := autoModel(t, &dark, &now)
	m.cfg.ThemeAuto = false
	now = now.Add(time.Hour)
	if cmd := m.appearanceCmd(); cmd != nil {
		t.Error("the appearance is polled with theme_auto off")
	}
	send(t, m, appearanceMsg{dark: false})
	if m.themeName != "nord" {
		t.Errorf("theme = %q, want nord left alone", m.themeName)
	}
}

// The tick drives the poll, so no separate timer is needed.
func TestSessionTickPollsAppearance(t *testing.T) {
	dark, now := false, time.Now()
	m := autoModel(t, &dark, &now)
	m.refreshInFlight = true // keep the tick from scanning real sessions
	now = now.Add(appearanceInterval)
	send(t, m, tickSessionMsg{})
	if !m.appearanceInFlight {
		t.Error("the session tick did not start an appearance check")
	}
}

func TestThemePickerSavesToTheAppearanceItWasPickedIn(t *testing.T) {
	for _, tc := range []struct {
		dark  bool
		field func(config.Config) string
		name  string
	}{
		{false, func(c config.Config) string { return c.ThemeLight }, "theme_light"},
		{true, func(c config.Config) string { return c.ThemeDark }, "theme_dark"},
	} {
		dark, now := tc.dark, time.Now()
		m := autoModel(t, &dark, &now)
		m.osDark = tc.dark
		var saved config.Config
		m.saveConfig = func(c config.Config) error { saved = c; return nil }

		typeKeys(t, m, KeyThemePicker)
		if m.mode != ModeThemePicker {
			t.Fatalf("mode = %v, want the theme picker", m.mode)
		}
		typeKeys(t, m, "j")
		picked := m.themePicker.SelectedKey()
		pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})

		if got := tc.field(saved); got != picked {
			t.Errorf("%s = %q, want the picked theme %q", tc.name, got, picked)
		}
		if saved.Theme != "nord" {
			t.Errorf("dark=%v: theme = %q, want nord untouched", tc.dark, saved.Theme)
		}
		if m.themeName != picked || m.mode != ModeList {
			t.Errorf("dark=%v: applied theme = %q, mode = %v", tc.dark, m.themeName, m.mode)
		}
	}
}

func TestThemePickerSavesThemeWhenAutoOff(t *testing.T) {
	dark, now := true, time.Now()
	m := autoModel(t, &dark, &now)
	m.cfg.ThemeAuto = false
	var saved config.Config
	m.saveConfig = func(c config.Config) error { saved = c; return nil }
	typeKeys(t, m, KeyThemePicker, "j")
	picked := m.themePicker.SelectedKey()
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if saved.Theme != picked || saved.ThemeDark != "" {
		t.Errorf("theme = %q, theme_dark = %q; want only theme set to %q", saved.Theme, saved.ThemeDark, picked)
	}
}

func TestThemePickerEscRestoresAppliedTheme(t *testing.T) {
	dark, now := false, time.Now()
	m := autoModel(t, &dark, &now)
	m.setTheme("catppuccin-latte") // what auto applied; cfg.Theme is still nord
	typeKeys(t, m, KeyThemePicker, "j", "j", "esc")
	if m.themeName != "catppuccin-latte" || styles.ColorBg != styles.Themes["catppuccin-latte"].Bg {
		t.Errorf("theme = %q, want the one applied before the picker opened", m.themeName)
	}
	if m.cfg.Theme != "nord" || m.cfg.ThemeLight != config.DefaultConfig().ThemeLight {
		t.Errorf("Esc changed the config: theme = %q, theme_light = %q", m.cfg.Theme, m.cfg.ThemeLight)
	}
}

// The picker previews its own choice; an appearance change waits for it to close.
func TestAppearanceChangeWaitsForOpenThemePicker(t *testing.T) {
	dark, now := true, time.Now()
	m := autoModel(t, &dark, &now)
	typeKeys(t, m, KeyThemePicker)
	send(t, m, appearanceMsg{dark: false})
	// The theme waits for the picker, but the mode is tracked so a save lands in theme_light.
	if m.themeName != "nord" || m.osDark {
		t.Errorf("theme = %q, osDark = %v; want the switch held back and the mode tracked", m.themeName, m.osDark)
	}
	typeKeys(t, m, "esc")
	send(t, m, appearanceMsg{dark: false})
	if m.themeName != "catppuccin-latte" {
		t.Errorf("theme = %q after the picker closed, want catppuccin-latte", m.themeName)
	}
}
