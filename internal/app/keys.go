package app

import (
	"fmt"
	"sort"

	"github.com/thbits/naviClaude/internal/config"
)

// KeyMap holds configurable key bindings loaded from config.
type KeyMap struct {
	Quit           string
	Search         string
	Focus          string
	Jump           string
	NewSession     string
	NewTmuxSession string
	KillSession    string
	RenameSession  string
	Detail         string
	Stats          string
	Help           string
	ToggleChanged  string
	MoveWindow     string
	PinSession     string
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:           "q",
		Search:         "/",
		Focus:          "enter",
		Jump:           "f",
		NewSession:     "n",
		NewTmuxSession: "N",
		KillSession:    "K",
		RenameSession:  "r",
		Detail:         "d",
		Stats:          "s",
		Help:           "?",
		ToggleChanged:  "F",
		MoveWindow:     "m",
		PinSession:     "p",
	}
}

// keyFieldBinding pairs a configured key with its KeyMap field.
type keyFieldBinding struct {
	action string  // human-readable action name (used for conflict reporting)
	from   string  // configured value from config.KeyBindings
	to     *string // destination field in the KeyMap being built
}

// keyFieldBindings is the config-to-KeyMap table shared by KeyMapFromConfig and ValidateKeys.
func keyFieldBindings(kb config.KeyBindings, km *KeyMap) []keyFieldBinding {
	return []keyFieldBinding{
		{"quit", kb.Quit, &km.Quit},
		{"search", kb.Search, &km.Search},
		{"focus", kb.Focus, &km.Focus},
		{"jump", kb.Jump, &km.Jump},
		{"new_session", kb.NewSession, &km.NewSession},
		{"new_tmux_session", kb.NewTmuxSession, &km.NewTmuxSession},
		{"kill_session", kb.KillSession, &km.KillSession},
		{"rename_session", kb.RenameSession, &km.RenameSession},
		{"detail", kb.Detail, &km.Detail},
		{"stats", kb.Stats, &km.Stats},
		{"help", kb.Help, &km.Help},
		{"toggle_changed", kb.ToggleChanged, &km.ToggleChanged},
		{"move_window", kb.MoveWindow, &km.MoveWindow},
		{"pin_session", kb.PinSession, &km.PinSession},
	}
}

// KeyMapFromConfig builds a KeyMap from config.KeyBindings, falling back to
// defaults for any empty values.
func KeyMapFromConfig(kb config.KeyBindings) KeyMap {
	km := DefaultKeyMap()
	for _, b := range keyFieldBindings(kb, &km) {
		if b.from != "" {
			*b.to = b.from
		}
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
		// Move around
		{"j/k", "Navigate sessions"},
		{"Tab/Shift+Tab", "Cycle panes"},
		{"Ctrl+U/Ctrl+D", "Scroll preview"},
		{km.Search, "Search"},
		// Act on the selected session
		{keyLabel(km.Focus), "Focus / resume closed"},
		{km.Jump, "Jump / resume closed, then jump"},
		{km.NewSession, "New session"},
		{km.NewTmuxSession, "New tmux session"},
		{km.RenameSession, "Rename session / tmux session (on a header)"},
		{km.MoveWindow, "Move to another tmux session"},
		{km.PinSession, "Pin / unpin a session (or, on a header, its tmux session) to the top"},
		{km.KillSession, "Kill session"},
		// Inspect the selected session
		{km.ToggleChanged, "Changed files panel"},
		{km.Detail, "Detail"},
		{km.Stats, "Stats"},
		// App
		{KeyThemePicker, "Theme picker"},
		{km.Help, "Help"},
		{km.Quit, "Quit"},
	}
}

// StatusHints returns the configured list-mode hints, most useful first; help comes
// last and is never dropped, and everything else lives in the help overlay.
func (km KeyMap) StatusHints() []HelpBinding {
	return []HelpBinding{
		{keyLabel(km.Focus), "open"},
		{km.Jump, "jump"},
		{km.NewSession, "new"},
		{km.Search, "search"},
		{km.ToggleChanged, "files"},
		{km.KillSession, "kill"},
		{km.Help, "more"},
	}
}

// keyLabel shows "enter" as "Enter", as the help and hints spell it; hint clicks
// match key names case-insensitively, so the label still replays the key.
func keyLabel(k string) string {
	if k == KeyEnter {
		return "Enter"
	}
	return k
}

// Non-configurable keys: passthrough exit, modal keys, navigation.
const (
	KeyTab             = "tab"
	KeyEnter           = "enter"
	KeyCtrlC           = "ctrl+c"
	KeyExitPassthrough = "ctrl+]"
	KeyCyclePanes      = "shift+tab"
	KeyJumpFromPT      = "ctrl+f"
	KeyMenuCancel      = "esc"
	KeyMenuSelect      = "enter"
	KeyThemePicker     = "T"
)

// reservedKeyBindings maps the hardcoded keys a configured binding would be
// shadowed by to what reserves them.
func reservedKeyBindings() map[string]string {
	return map[string]string{
		KeyThemePicker:     "theme picker",
		KeyTab:             "pane cycling",
		KeyExitPassthrough: "passthrough exit",
		KeyCyclePanes:      "pane cycling / passthrough exit",
		KeyMenuCancel:      "menu cancel",
		KeyCtrlC:           "force quit",
		KeyJumpFromPT:      "jump from passthrough",
	}
}

// KeyConflict describes a single key-binding conflict detected in the config.
type KeyConflict struct {
	Key     string // the conflicting key (tea.KeyMsg.String() format)
	Message string // human-readable explanation
}

// String renders the conflict for display or logging.
func (c KeyConflict) String() string { return c.Message }

// ValidateKeys reports configured keys bound to several actions or shadowed by a
// reserved key.
func ValidateKeys(kb config.KeyBindings) []KeyConflict {
	var km KeyMap
	bindings := keyFieldBindings(kb, &km)
	reserved := reservedKeyBindings()

	// Track which actions have claimed each configured key.
	claimed := make(map[string][]string)
	for _, b := range bindings {
		if b.from == "" {
			continue // empty means "use default"; not a configured binding
		}
		claimed[b.from] = append(claimed[b.from], b.action)
	}

	var conflicts []KeyConflict

	// Duplicate configured bindings: same key bound by 2+ actions.
	keys := make([]string, 0, len(claimed))
	for k := range claimed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		actions := claimed[k]
		if len(actions) > 1 {
			sort.Strings(actions)
			conflicts = append(conflicts, KeyConflict{
				Key: k,
				Message: fmt.Sprintf("key %q is bound to multiple actions: %v",
					k, actions),
			})
		}
		// Collision with a reserved/hardcoded key.
		if desc, ok := reserved[k]; ok {
			sort.Strings(actions)
			conflicts = append(conflicts, KeyConflict{
				Key: k,
				Message: fmt.Sprintf("key %q (reserved for %s) is bound to %v",
					k, desc, actions),
			})
		}
	}

	return conflicts
}
