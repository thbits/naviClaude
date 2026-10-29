package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// validSortOrders is the set of allowed values for the group/session sort-order
// enums. Anything outside this set falls back to the field default.
var validSortOrders = map[string]bool{
	"name":     true,
	"activity": true,
	"manual":   true, // set by dragging rows in the session list
}

// Config holds all user-configurable settings for naviClaude.
type Config struct {
	Keys               KeyBindings   `yaml:"keys"`
	SidebarWidth       int           `yaml:"sidebar_width"`
	RefreshInterval    string        `yaml:"refresh_interval"`
	ClosedSessionHours float64       `yaml:"closed_session_hours"`
	PopupWidth         int           `yaml:"popup_width"`
	PopupHeight        int           `yaml:"popup_height"`
	ResumeInCurrent    bool          `yaml:"resume_in_current_session"`
	ProcessNames       []string      `yaml:"process_names"`
	CollapseAfterHours float64       `yaml:"collapse_after_hours"` // auto-collapse groups idle longer than this (0 = disabled)
	ActiveWindowSecs   int           `yaml:"active_window_secs"`   // seconds after last .jsonl write to keep session "active" (default 5)
	CPUActiveThreshold float64       `yaml:"cpu_active_threshold"` // process-subtree CPU% above which a session counts as working (default 5)
	Theme              string        `yaml:"theme"`                // color theme name (default "tokyo-night")
	ClaudeCommand      string        `yaml:"claude_command"`       // command to start Claude in new sessions (default "claude")
	WindowName         string        `yaml:"window_name"`          // tmux window name for new/resumed sessions; {project} and {harness} expand (default "{project}")
	NewSessionDir      string        `yaml:"new_session_dir"`      // working directory for new tmux sessions (default "~")
	GroupSortOrder     string        `yaml:"group_sort_order"`     // "name" (alphabetical, default), "activity" or "manual" (dragged order)
	SessionSortOrder   string        `yaml:"session_sort_order"`   // "name" (alphabetical, default), "activity" or "manual" (tmux window order)
	CheckForUpdates    bool          `yaml:"check_for_updates"`    // check GitHub for a newer release on startup (default true)
	Editor             string        `yaml:"editor"`               // editor for opening changed files; empty falls back to $EDITOR, then vi
	FocusLastSession   bool          `yaml:"focus_last_session"`   // re-select the last focused session on startup (default true)
	RememberGroupState bool          `yaml:"remember_group_state"` // restore manually-toggled group open/closed state on startup (default true)
	RestoreSessions    bool          `yaml:"restore_sessions"`     // resume sessions killed by a tmux restart on the next launch (default true)
	Notifications      Notifications `yaml:"notifications"`
}

// Notifications configures the Claude Code hooks. A sound is any file afplay can
// play, "" for the built-in one, or "none" to silence that event.
type Notifications struct {
	Enabled              bool    `yaml:"enabled"`                 // master switch (default true)
	RequestSound         string  `yaml:"request_sound"`           // played when Claude needs input
	DoneSound            string  `yaml:"done_sound"`              // played when a turn finishes in the background
	DoneSoundWhenFocused bool    `yaml:"done_sound_when_focused"` // also play done when you are already looking at the pane
	Volume               float64 `yaml:"volume"`                  // afplay volume, 0-1 (default 1)
	Cooldown             string  `yaml:"cooldown"`                // minimum gap between sounds across all sessions (default "2s")
	WindowMarker         bool    `yaml:"window_marker"`           // suffix the tmux window name while a pane needs you (default true)
	WaitingMarker        string  `yaml:"waiting_marker"`          // window-name suffix for "needs input"; may hold tmux #[...] styles
	DoneMarker           string  `yaml:"done_marker"`             // window-name suffix for "finished, unseen"; may hold tmux #[...] styles
	BusyMarker           string  `yaml:"busy_marker"`             // window-name suffix while Claude is working; "none" hides it
}

// KeyBindings maps user-facing actions to key names (tea.KeyMsg.String() format).
type KeyBindings struct {
	Focus          string `yaml:"focus"`
	Jump           string `yaml:"jump"`
	Search         string `yaml:"search"`
	NewSession     string `yaml:"new_session"`
	NewTmuxSession string `yaml:"new_tmux_session"`
	KillSession    string `yaml:"kill_session"`
	RenameSession  string `yaml:"rename_session"`
	Detail         string `yaml:"detail"`
	Stats          string `yaml:"stats"`
	Help           string `yaml:"help"`
	Quit           string `yaml:"quit"`
	ToggleChanged  string `yaml:"toggle_changed"`
	MoveWindow     string `yaml:"move_window"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Keys: KeyBindings{
			Focus:          "enter",
			Jump:           "f",
			Search:         "/",
			NewSession:     "n",
			NewTmuxSession: "N",
			KillSession:    "K",
			RenameSession:  "r",
			Detail:         "d",
			Stats:          "s",
			Help:           "?",
			Quit:           "q",
			ToggleChanged:  "F",
			MoveWindow:     "m",
		},
		SidebarWidth:       30,
		RefreshInterval:    "200ms",
		ClosedSessionHours: 8,
		PopupWidth:         85,
		PopupHeight:        85,
		ResumeInCurrent:    true,
		ProcessNames:       []string{"claude"},
		CollapseAfterHours: 8,
		ActiveWindowSecs:   5,
		CPUActiveThreshold: 5.0,
		Theme:              "tokyo-night",
		ClaudeCommand:      "claude",
		WindowName:         "{project}",
		GroupSortOrder:     "name",
		SessionSortOrder:   "name",
		CheckForUpdates:    true,
		FocusLastSession:   true,
		RememberGroupState: true,
		RestoreSessions:    true,
		Notifications: Notifications{
			Enabled:       true,
			Volume:        1,
			Cooldown:      "2s",
			WindowMarker:  true,
			WaitingMarker: "#[fg=yellow,bold]\uf0f3#[nobold]", // nerd font bell
			DoneMarker:    "#[fg=green,bold]\uf00c#[nobold]",  // nerd font check
			BusyMarker:    "#[fg=blue,bold]\uf110#[nobold]",   // nerd font spinner
		},
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

	sanitizeConfig(&cfg)

	return cfg, nil
}

// sanitizeConfig replaces unset (zero), negative or invalid fields with their
// DefaultConfig value, so an explicit numeric 0 is never honored.
func sanitizeConfig(cfg *Config) {
	d := DefaultConfig()

	// Numeric fields: zero or negative means unset.
	if cfg.SidebarWidth <= 0 {
		cfg.SidebarWidth = d.SidebarWidth
	}
	if cfg.ClosedSessionHours <= 0 {
		cfg.ClosedSessionHours = d.ClosedSessionHours
	}
	if cfg.PopupWidth <= 0 {
		cfg.PopupWidth = d.PopupWidth
	}
	if cfg.PopupHeight <= 0 {
		cfg.PopupHeight = d.PopupHeight
	}
	if cfg.ActiveWindowSecs <= 0 {
		cfg.ActiveWindowSecs = d.ActiveWindowSecs
	}
	if cfg.CPUActiveThreshold <= 0 {
		cfg.CPUActiveThreshold = d.CPUActiveThreshold
	}
	// CollapseAfterHours uses 0 as a sentinel ("disabled"); only a negative
	// value is invalid, so do not coerce a legitimate 0.
	if cfg.CollapseAfterHours < 0 {
		cfg.CollapseAfterHours = d.CollapseAfterHours
	}

	// String / slice fields: empty falls back to default.
	if len(cfg.ProcessNames) == 0 {
		cfg.ProcessNames = d.ProcessNames
	}
	if cfg.Theme == "" {
		cfg.Theme = d.Theme
	}
	if cfg.WindowName == "" {
		cfg.WindowName = d.WindowName
	}
	if cfg.ClaudeCommand == "" {
		cfg.ClaudeCommand = d.ClaudeCommand
	}

	// RefreshInterval must parse as a time.Duration; otherwise fall back.
	if _, err := time.ParseDuration(cfg.RefreshInterval); err != nil {
		cfg.RefreshInterval = d.RefreshInterval
	}

	n := &cfg.Notifications
	if n.Volume <= 0 || n.Volume > 1 {
		n.Volume = d.Notifications.Volume
	}
	if _, err := time.ParseDuration(n.Cooldown); err != nil {
		n.Cooldown = d.Notifications.Cooldown
	}
	if n.WaitingMarker == "" {
		n.WaitingMarker = d.Notifications.WaitingMarker
	}
	if n.DoneMarker == "" {
		n.DoneMarker = d.Notifications.DoneMarker
	}
	if n.BusyMarker == "" {
		n.BusyMarker = d.Notifications.BusyMarker
	}

	// Sort-order enums must be one of the allowed values; otherwise fall back.
	if !validSortOrders[cfg.GroupSortOrder] {
		cfg.GroupSortOrder = d.GroupSortOrder
	}
	if !validSortOrders[cfg.SessionSortOrder] {
		cfg.SessionSortOrder = d.SessionSortOrder
	}
}
