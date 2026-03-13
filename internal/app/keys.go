package app

import "github.com/thbits/naviClaude/internal/config"

// KeyMap holds configurable key bindings loaded from config.
type KeyMap struct {
	Quit        string
	Search      string
	Focus       string
	Jump        string
	NewSession  string
	KillSession string
	Detail      string
	Stats       string
	Help        string
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:        "q",
		Search:      "/",
		Focus:       "enter",
		Jump:        "f",
		NewSession:  "n",
		KillSession: "K",
		Detail:      "d",
		Stats:       "s",
		Help:        "?",
	}
}

// KeyMapFromConfig builds a KeyMap from config.KeyBindings, falling back to
// defaults for any empty values.
func KeyMapFromConfig(kb config.KeyBindings) KeyMap {
	km := DefaultKeyMap()
	if kb.Quit != "" {
		km.Quit = kb.Quit
	}
	if kb.Search != "" {
		km.Search = kb.Search
	}
	if kb.Focus != "" {
		km.Focus = kb.Focus
	}
	if kb.Jump != "" {
		km.Jump = kb.Jump
	}
	if kb.NewSession != "" {
		km.NewSession = kb.NewSession
	}
	if kb.KillSession != "" {
		km.KillSession = kb.KillSession
	}
	if kb.Detail != "" {
		km.Detail = kb.Detail
	}
	if kb.Stats != "" {
		km.Stats = kb.Stats
	}
	if kb.Help != "" {
		km.Help = kb.Help
	}
	return km
}

// HelpBinding is a key-description pair for the help overlay.
type HelpBinding struct {
	Key  string
	Desc string
}

// HelpBindings returns bindings for the help overlay, reflecting the current config.
func (km KeyMap) HelpBindings() []HelpBinding {
	return []HelpBinding{
		{"j/k", "Navigate sessions"},
		{"Enter/Tab", "Focus (passthrough)"},
		{km.Jump, "Jump to pane"},
		{km.Search, "Search"},
		{km.NewSession, "New session"},
		{km.KillSession, "Kill session"},
		{km.Detail, "Detail"},
		{km.Stats, "Stats"},
		{km.Help, "Help"},
		{km.Quit, "Quit"},
	}
}

// StatusHints returns the list-mode status bar hints reflecting the current config.
func (km KeyMap) StatusHints() []HelpBinding {
	return []HelpBinding{
		{"Enter", "focus"},
		{km.Jump, "jump"},
		{km.Search, "search"},
		{km.NewSession, "new"},
		{km.KillSession, "kill"},
		{km.Detail, "detail"},
		{km.Stats, "stats"},
		{km.Help, "help"},
	}
}

// Non-configurable keys: passthrough exit, modal keys, navigation.
const (
	KeyTab              = "tab"
	KeyEnter            = "enter"
	KeyCtrlC            = "ctrl+c"
	KeyExitPassthrough  = "tab"
	KeyExitPassthrough2 = "ctrl+]"
	KeyJumpFromPT       = "ctrl+f"
	KeyMenuCancel       = "esc"
	KeyMenuSelect       = "enter"
)
