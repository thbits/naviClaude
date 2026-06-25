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
	}
}

// keyFieldBinding pairs a configured value (from config.KeyBindings) with the
// destination field in the resulting KeyMap, so the per-field fallback logic in
// KeyMapFromConfig can be table-driven instead of repeated by hand.
type keyFieldBinding struct {
	action string  // human-readable action name (used for conflict reporting)
	from   string  // configured value from config.KeyBindings
	to     *string // destination field in the KeyMap being built
}

// keyFieldBindings returns the action/source/destination table linking each
// config.KeyBindings field to its KeyMap field. It is the single source of
// truth shared by KeyMapFromConfig and ValidateKeys.
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
		{"j/k", "Navigate sessions"},
		{"Enter/Tab", "Focus / resume closed"},
		{km.Jump, "Jump / fast-resume closed"},
		{"Ctrl+U/Ctrl+D", "Scroll preview"},
		{km.Search, "Search"},
		{km.NewSession, "New session"},
		{km.NewTmuxSession, "New tmux session"},
		{km.KillSession, "Kill session"},
		{km.RenameSession, "Rename session"},
		{km.Detail, "Detail"},
		{km.Stats, "Stats"},
		{KeyThemePicker, "Theme picker"},
		{km.Help, "Help"},
		{km.Quit, "Quit"},
	}
}

// StatusHints returns the list-mode status bar hints reflecting the current config.
func (km KeyMap) StatusHints() []HelpBinding {
	return []HelpBinding{
		{"Enter", "focus/resume"},
		{km.Jump, "jump"},
		{km.Search, "search"},
		{km.NewSession, "new"},
		{km.NewTmuxSession, "new tmux session"},
		{km.KillSession, "kill"},
		{km.RenameSession, "rename"},
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
	KeyExitPassthrough3 = "shift+tab"
	KeyJumpFromPT       = "ctrl+f"
	KeyMenuCancel       = "esc"
	KeyMenuSelect       = "enter"
	KeyThemePicker      = "T"
)

// reservedKeyBindings returns the hardcoded keys that configured bindings must
// not shadow. These are handled directly by the update loop regardless of
// config, so binding a configurable action to one of them would either be
// ignored or behave unpredictably. Each entry maps the key to a description of
// what reserves it, for clear conflict messages.
func reservedKeyBindings() map[string]string {
	return map[string]string{
		KeyThemePicker:      "theme picker",
		KeyTab:              "focus / passthrough exit",
		KeyExitPassthrough2: "passthrough exit",
		KeyExitPassthrough3: "passthrough exit",
		KeyMenuCancel:       "menu cancel",
		KeyCtrlC:            "force quit",
		KeyJumpFromPT:       "jump from passthrough",
	}
}

// KeyConflict describes a single key-binding conflict detected in the config.
type KeyConflict struct {
	Key     string // the conflicting key (tea.KeyMsg.String() format)
	Message string // human-readable explanation
}

// String renders the conflict for display or logging.
func (c KeyConflict) String() string { return c.Message }

// ValidateKeys checks a config.KeyBindings for conflicts: two configured
// actions bound to the same key, and configured actions that shadow a
// reserved/hardcoded key. It returns the list of conflicts (empty if none).
//
// It does not mutate anything and is independent of KeyMapFromConfig, so
// callers can surface warnings without changing how the KeyMap is built.
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
