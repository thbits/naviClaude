package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultStatusKeysAndAutoTheme(t *testing.T) {
	cfg := DefaultConfig()
	for name, got := range map[string][2]string{
		"detail":         {cfg.Keys.Detail, "D"},
		"next_attention": {cfg.Keys.NextAttention, "space"},
		"filter_waiting": {cfg.Keys.FilterWaiting, "w"},
		"filter_done":    {cfg.Keys.FilterDone, "d"},
		"filter_working": {cfg.Keys.FilterWorking, "a"},
		"filter_idle":    {cfg.Keys.FilterIdle, "i"},
		"preview_search": {cfg.Keys.PreviewSearch, "S"},
		"theme_light":    {cfg.ThemeLight, "catppuccin-latte"},
		"theme_dark":     {cfg.ThemeDark, ""},
	} {
		if got[0] != got[1] {
			t.Errorf("default %s = %q, want %q", name, got[0], got[1])
		}
	}
	if cfg.ThemeAuto {
		t.Error("theme_auto must be opt-in")
	}
}

// A file written before these keys existed gets their defaults, and the theme
// settings round-trip.
func TestLoadFillsNewKeysAndReadsAutoTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "keys:\n  detail: \"d\"\ntheme_auto: true\ntheme_light: \"rose-pine-dawn\"\ntheme_dark: \"nord\"\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keys.FilterDone != "d" || cfg.Keys.NextAttention != "space" || cfg.Keys.PreviewSearch != "S" {
		t.Errorf("new keys not defaulted: %+v", cfg.Keys)
	}
	if !cfg.ThemeAuto || cfg.ThemeLight != "rose-pine-dawn" || cfg.ThemeDark != "nord" {
		t.Errorf("auto theme = %v / %q / %q", cfg.ThemeAuto, cfg.ThemeLight, cfg.ThemeDark)
	}

	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.ThemeAuto != cfg.ThemeAuto || again.ThemeLight != cfg.ThemeLight || again.ThemeDark != cfg.ThemeDark || again.Keys != cfg.Keys {
		t.Errorf("round trip changed the config:\n got %+v\nwant %+v", again, cfg)
	}
}
