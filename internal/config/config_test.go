package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"SidebarWidth", cfg.SidebarWidth, 30},
		{"RefreshInterval", cfg.RefreshInterval, "200ms"},
		{"ClosedSessionHours", cfg.ClosedSessionHours, 8.0},
		{"PopupWidth", cfg.PopupWidth, 85},
		{"PopupHeight", cfg.PopupHeight, 85},
		{"ResumeInCurrent", cfg.ResumeInCurrent, true},
		{"CollapseAfterHours", cfg.CollapseAfterHours, 8.0},
		{"ActiveWindowSecs", cfg.ActiveWindowSecs, 5},
		{"Theme", cfg.Theme, "tokyo-night"},
		{"Keys.Focus", cfg.Keys.Focus, "enter"},
		{"Keys.Quit", cfg.Keys.Quit, "q"},
		{"Keys.Search", cfg.Keys.Search, "/"},
		{"Keys.KillSession", cfg.Keys.KillSession, "K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use sprint comparison to handle different numeric types.
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}

	if len(cfg.ProcessNames) != 1 || cfg.ProcessNames[0] != "claude" {
		t.Errorf("ProcessNames = %v, want [claude]", cfg.ProcessNames)
	}
}

func TestLoadNonexistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	def := DefaultConfig()
	if cfg.SidebarWidth != def.SidebarWidth {
		t.Errorf("SidebarWidth = %d, want default %d", cfg.SidebarWidth, def.SidebarWidth)
	}
	if cfg.Theme != def.Theme {
		t.Errorf("Theme = %q, want default %q", cfg.Theme, def.Theme)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig()
	cfg.SidebarWidth = 50
	cfg.Theme = "catppuccin"
	cfg.RefreshInterval = "500ms"

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if loaded.SidebarWidth != 50 {
		t.Errorf("SidebarWidth = %d, want 50", loaded.SidebarWidth)
	}
	if loaded.Theme != "catppuccin" {
		t.Errorf("Theme = %q, want %q", loaded.Theme, "catppuccin")
	}
	if loaded.RefreshInterval != "500ms" {
		t.Errorf("RefreshInterval = %q, want %q", loaded.RefreshInterval, "500ms")
	}
}

func TestLoadZeroValueDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write a config with zero-value fields that should be overridden by defaults.
	yaml := "sidebar_width: 0\nclosed_session_hours: 0\npopup_width: 0\npopup_height: 0\nactive_window_secs: 0\ntheme: \"\"\n"
	os.WriteFile(path, []byte(yaml), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.SidebarWidth != 30 {
		t.Errorf("SidebarWidth = %d, want default 30", cfg.SidebarWidth)
	}
	if cfg.ClosedSessionHours != 8 {
		t.Errorf("ClosedSessionHours = %v, want default 8", cfg.ClosedSessionHours)
	}
	if cfg.PopupWidth != 85 {
		t.Errorf("PopupWidth = %d, want default 85", cfg.PopupWidth)
	}
	if cfg.PopupHeight != 85 {
		t.Errorf("PopupHeight = %d, want default 85", cfg.PopupHeight)
	}
	if cfg.ActiveWindowSecs != 5 {
		t.Errorf("ActiveWindowSecs = %d, want default 5", cfg.ActiveWindowSecs)
	}
	if cfg.Theme != "tokyo-night" {
		t.Errorf("Theme = %q, want default %q", cfg.Theme, "tokyo-night")
	}
	if len(cfg.ProcessNames) != 1 || cfg.ProcessNames[0] != "claude" {
		t.Errorf("ProcessNames = %v, want default [claude]", cfg.ProcessNames)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Use a YAML document that will fail to unmarshal into Config struct:
	// a bare tab character at the start of line after a mapping key is invalid.
	os.WriteFile(path, []byte("keys:\n\t- bad\n\t- indentation"), 0o644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadPartialOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := "sidebar_width: 40\ntheme: dracula\n"
	os.WriteFile(path, []byte(yaml), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Overridden values.
	if cfg.SidebarWidth != 40 {
		t.Errorf("SidebarWidth = %d, want 40", cfg.SidebarWidth)
	}
	if cfg.Theme != "dracula" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "dracula")
	}

	// Non-overridden values retain defaults.
	def := DefaultConfig()
	if cfg.PopupWidth != def.PopupWidth {
		t.Errorf("PopupWidth = %d, want default %d", cfg.PopupWidth, def.PopupWidth)
	}
	if cfg.Keys.Quit != def.Keys.Quit {
		t.Errorf("Keys.Quit = %q, want default %q", cfg.Keys.Quit, def.Keys.Quit)
	}
}

func TestSaveCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "config.yaml")

	if err := Save(DefaultConfig(), path); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected file to be created")
	}
}
