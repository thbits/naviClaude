package app

// Hardcoded keybinding constants for Phase 1. These are the key names as
// returned by tea.KeyMsg.String(). A future phase may load these from a
// config file.

// List mode keys.
const (
	KeyUp       = "up"
	KeyDown     = "down"
	KeyVimUp    = "k"
	KeyVimDown  = "j"
	KeyTop      = "g"
	KeyBottom   = "G"
	KeyEnter    = "enter"
	KeyTab      = "tab"
	KeyFocus    = "f"    // jump to pane
	KeySearch   = "/"    // open search
	KeyNew      = "n"    // create new session
	KeyKill     = "K"    // kill session
	KeyDetail   = "d"    // show detail (Phase 2)
	KeyStats    = "s"    // show stats (Phase 2)
	KeyHelp     = "?"    // toggle help overlay
	KeyQuit     = "q"    // quit naviClaude
	KeyCtrlC    = "ctrl+c"
)

// Passthrough mode keys.
const (
	KeyExitPassthrough  = "tab"
	KeyExitPassthrough2 = "ctrl+]"
	KeyJumpFromPT       = "ctrl+f" // jump to pane from passthrough
)

// Search mode keys (handled by SearchModel, listed here for reference).
const (
	KeySearchCancel = "esc"
	KeySearchSelect = "enter"
)

// Context menu keys (handled by ContextMenuModel, listed here for reference).
const (
	KeyMenuCancel = "esc"
	KeyMenuSelect = "enter"
)
