package app

// Mode represents the current input mode of the application.
type Mode int

const (
	// ModeList is the default mode where the sidebar has focus and the user
	// navigates sessions with j/k, opens passthrough with Enter, etc.
	ModeList Mode = iota

	// ModePassthrough forwards all key presses (except the exit combo) to the
	// selected session's tmux pane via send-keys.
	ModePassthrough

	// ModeSearch activates the fuzzy search overlay. Typing filters sessions
	// and Enter selects the highlighted result.
	ModeSearch

	// ModeContextMenu shows a floating menu of actions for a session.
	ModeContextMenu

	// ModeHelp displays a full-screen keybinding reference overlay.
	ModeHelp

	// ModeDetail shows a centered popup with session metadata.
	ModeDetail

	// ModeStats shows a centered popup with usage statistics.
	ModeStats
)

// String returns a short label suitable for the status bar.
func (m Mode) String() string {
	switch m {
	case ModeList:
		return "list"
	case ModePassthrough:
		return "passthrough"
	case ModeSearch:
		return "search"
	case ModeContextMenu:
		return "contextmenu"
	case ModeHelp:
		return "help"
	case ModeDetail:
		return "detail"
	case ModeStats:
		return "stats"
	default:
		return "unknown"
	}
}
