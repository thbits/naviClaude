package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all user-configurable settings for naviClaude.
type Config struct {
	Keys               KeyBindings `yaml:"keys"`
	SidebarWidth       int         `yaml:"sidebar_width"`
	RefreshInterval    string      `yaml:"refresh_interval"`
	ClosedSessionHours float64     `yaml:"closed_session_hours"`
	PopupWidth         int         `yaml:"popup_width"`
	PopupHeight        int         `yaml:"popup_height"`
	ResumeInCurrent    bool        `yaml:"resume_in_current_session"`
	ProcessNames       []string    `yaml:"process_names"`
	CollapseAfterHours float64     `yaml:"collapse_after_hours"`  // auto-collapse groups idle longer than this (0 = disabled)
	ActiveWindowSecs   int         `yaml:"active_window_secs"`    // seconds after last .jsonl write to keep session "active" (default 5)
	Theme              string      `yaml:"theme"`                 // color theme name (default "tokyo-night")
	ClaudeCommand      string      `yaml:"claude_command"`        // command to start Claude in new sessions (default "claude")
	NewSessionDir      string      `yaml:"new_session_dir"`       // working directory for new tmux sessions (default "~")
}

// KeyBindings maps user-facing actions to key names (tea.KeyMsg.String() format).
type KeyBindings struct {
	Focus       string `yaml:"focus"`
	Jump        string `yaml:"jump"`
	Search      string `yaml:"search"`
	NewSession      string `yaml:"new_session"`
	NewTmuxSession  string `yaml:"new_tmux_session"`
	KillSession string `yaml:"kill_session"`
	Detail      string `yaml:"detail"`
	Stats       string `yaml:"stats"`
	Help        string `yaml:"help"`
	Quit        string `yaml:"quit"`
}

// DefaultConfig returns the default configuration matching Phase 1 behavior.
func DefaultConfig() Config {
	return Config{
		Keys: KeyBindings{
			Focus:       "enter",
			Jump:        "f",
			Search:      "/",
			NewSession:     "n",
			NewTmuxSession: "N",
			KillSession: "K",
			Detail:      "d",
			Stats:       "s",
			Help:        "?",
			Quit:        "q",
		},
		SidebarWidth:       30,
		RefreshInterval:    "200ms",
		ClosedSessionHours: 6,
		PopupWidth:         85,
		PopupHeight:        85,
		ResumeInCurrent:    true,
		ProcessNames:       []string{"claude"},
		CollapseAfterHours: 8,
		ActiveWindowSecs:   5,
		Theme:              "tokyo-night",
		ClaudeCommand:      "claude",
	}
}

// DefaultPath returns ~/.config/naviclaude/config.yaml.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "naviclaude", "config.yaml")
}

// Save writes cfg as YAML to path. Creates the file and parent directories
// if they do not exist. If path is empty, DefaultPath() is used.
func Save(cfg Config, path string) error {
	if path == "" {
		path = DefaultPath()
	}
	if path == "" {
		return fmt.Errorf("config: cannot determine config path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// Load reads a YAML config file and returns a Config. Missing fields retain
// their default values. If the file does not exist, DefaultConfig is returned.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		path = DefaultPath()
	}
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), err
	}

	// Ensure zero-value overrides don't break defaults.
	if cfg.SidebarWidth == 0 {
		cfg.SidebarWidth = 30
	}
	if cfg.ClosedSessionHours == 0 {
		cfg.ClosedSessionHours = 6
	}
	if cfg.PopupWidth == 0 {
		cfg.PopupWidth = 85
	}
	if cfg.PopupHeight == 0 {
		cfg.PopupHeight = 85
	}
	if len(cfg.ProcessNames) == 0 {
		cfg.ProcessNames = []string{"claude"}
	}
	if cfg.ActiveWindowSecs == 0 {
		cfg.ActiveWindowSecs = 5
	}
	if cfg.Theme == "" {
		cfg.Theme = "tokyo-night"
	}
	if cfg.ClaudeCommand == "" {
		cfg.ClaudeCommand = "claude"
	}

	return cfg, nil
}
