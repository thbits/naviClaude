// Package styles defines all Lipgloss visual styles for naviClaude.
// Tokyo Night color scheme throughout. No logic lives here -- only style
// definitions and color constants.
package styles

import "github.com/charmbracelet/lipgloss"

// ---------------------------------------------------------------------------
// Tokyo Night color palette
// ---------------------------------------------------------------------------

const (
	ColorBg        = lipgloss.Color("#1a1b26")
	ColorFg        = lipgloss.Color("#c0caf5")
	ColorSelection = lipgloss.Color("#283457")
	ColorBlue      = lipgloss.Color("#7aa2f7")
	ColorGreen     = lipgloss.Color("#9ece6a")
	ColorAmber     = lipgloss.Color("#e0af68")
	ColorRed       = lipgloss.Color("#f7768e")
	ColorGray      = lipgloss.Color("#565f89")
	ColorPurple    = lipgloss.Color("#bb9af7")
	ColorCyan      = lipgloss.Color("#7dcfff")
	ColorBorder    = lipgloss.Color("#3b4261")
	ColorDim       = lipgloss.Color("#414868")
)

// ---------------------------------------------------------------------------
// Sidebar styles
// ---------------------------------------------------------------------------

// SidebarItem is a normal (unselected) session entry in the sidebar.
var SidebarItem = lipgloss.NewStyle().
	Foreground(ColorFg).
	PaddingLeft(2)

// SidebarItemSelected is the highlighted session entry.
var SidebarItemSelected = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Background(ColorSelection).
	PaddingLeft(2).
	Bold(true)

// SidebarGroupHeader is the tmux session name row (collapsed/expanded toggle).
var SidebarGroupHeader = lipgloss.NewStyle().
	Foreground(ColorPurple).
	Bold(true).
	PaddingLeft(1)

// SidebarGroupCount renders the session count badge next to a group header.
var SidebarGroupCount = lipgloss.NewStyle().
	Foreground(ColorGray)

// SidebarProjectName is the project name within a session row.
var SidebarProjectName = lipgloss.NewStyle().
	Foreground(ColorFg)

// SidebarTime is the relative timestamp displayed next to a session.
var SidebarTime = lipgloss.NewStyle().
	Foreground(ColorGray)

// SidebarSummary is the truncated first-prompt summary line beneath a session.
var SidebarSummary = lipgloss.NewStyle().
	Foreground(ColorGray).
	PaddingLeft(4)

// Status icon styles -- one per SessionStatus value.

// StatusIconActive is the icon for a session that is actively producing output.
var StatusIconActive = lipgloss.NewStyle().
	Foreground(ColorGreen)

// StatusIconWaiting is the icon for a session waiting for user input.
var StatusIconWaiting = lipgloss.NewStyle().
	Foreground(ColorAmber)

// StatusIconIdle is the icon for a session that is idle (shell prompt visible).
var StatusIconIdle = lipgloss.NewStyle().
	Foreground(ColorGray)

// StatusIconClosed is the icon for a historical (closed) session.
var StatusIconClosed = lipgloss.NewStyle().
	Foreground(ColorDim)

// SidebarWaitingFlash is applied to a session row during the ~2-second flash
// that fires when a session transitions from active to waiting.
var SidebarWaitingFlash = lipgloss.NewStyle().
	Foreground(ColorBg).
	Background(ColorAmber).
	PaddingLeft(2).
	Bold(true)

// ---------------------------------------------------------------------------
// Preview panel styles
// ---------------------------------------------------------------------------

// PreviewBorderUnfocused is the border style for the preview panel when the
// sidebar is focused (list mode -- dim, unobtrusive).
var PreviewBorderUnfocused = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBorder)

// PreviewBorderFocused is the border style when the preview panel is in
// passthrough mode (blue, visually prominent).
var PreviewBorderFocused = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBlue)

// PreviewHeader is the bar at the top of the preview showing project name,
// branch, status badge, tmux target, and resource stats.
var PreviewHeader = lipgloss.NewStyle().
	Background(ColorDim).
	Foreground(ColorFg).
	PaddingLeft(1).
	PaddingRight(1)

// PreviewHeaderLabel is a dimmer label within the header (e.g., "CPU", "MEM").
var PreviewHeaderLabel = lipgloss.NewStyle().
	Foreground(ColorGray).
	Background(ColorDim)

// PreviewHeaderValue is a value within the header (e.g., the percentage).
var PreviewHeaderValue = lipgloss.NewStyle().
	Foreground(ColorCyan).
	Background(ColorDim)

// PreviewHeaderBranch highlights the git branch name.
var PreviewHeaderBranch = lipgloss.NewStyle().
	Foreground(ColorPurple).
	Background(ColorDim)

// PreviewPassthroughBadge is the "PASSTHROUGH" indicator badge shown when the
// preview panel is in passthrough mode.
var PreviewPassthroughBadge = lipgloss.NewStyle().
	Foreground(ColorBg).
	Background(ColorBlue).
	PaddingLeft(1).
	PaddingRight(1).
	Bold(true)

// PreviewContent is the base style for the terminal content area inside the
// preview (the viewport body).
var PreviewContent = lipgloss.NewStyle().
	Foreground(ColorFg)

// ---------------------------------------------------------------------------
// Status bar styles
// ---------------------------------------------------------------------------

// StatusBar is the full-width bar at the bottom of the screen.
var StatusBar = lipgloss.NewStyle().
	Background(ColorDim).
	Foreground(ColorFg)

// StatusBarKey is a single key hint label (e.g., "Enter").
var StatusBarKey = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Background(ColorDim).
	Bold(true)

// StatusBarDesc is the action description following a key hint (e.g., "focus").
var StatusBarDesc = lipgloss.NewStyle().
	Foreground(ColorGray).
	Background(ColorDim)

// StatusBarSep is the separator between hint pairs (e.g., " | ").
var StatusBarSep = lipgloss.NewStyle().
	Foreground(ColorBorder).
	Background(ColorDim)

// StatusBarVersion is the version string anchored to the right of the status bar.
var StatusBarVersion = lipgloss.NewStyle().
	Foreground(ColorGray).
	Background(ColorDim)

// ---------------------------------------------------------------------------
// Overlay / popup styles
// ---------------------------------------------------------------------------

// HelpBorder is the outer border of the help popup.
var HelpBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorPurple).
	Padding(1, 2)

// HelpTitle is the title bar inside the help popup.
var HelpTitle = lipgloss.NewStyle().
	Foreground(ColorPurple).
	Bold(true).
	MarginBottom(1)

// HelpKey renders a keybinding within the help popup.
var HelpKey = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

// HelpDesc renders the description for a keybinding.
var HelpDesc = lipgloss.NewStyle().
	Foreground(ColorFg)

// ContextMenuBorder is the border of the right-click context menu.
var ContextMenuBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBorder)

// ContextMenuItem is an unselected item in the context menu.
var ContextMenuItem = lipgloss.NewStyle().
	Foreground(ColorFg).
	PaddingLeft(1).
	PaddingRight(1)

// ContextMenuItemSelected is the highlighted item in the context menu.
var ContextMenuItemSelected = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Background(ColorSelection).
	PaddingLeft(1).
	PaddingRight(1).
	Bold(true)

// ContextMenuItemDanger highlights destructive actions (e.g., Kill) in red.
var ContextMenuItemDanger = lipgloss.NewStyle().
	Foreground(ColorRed).
	PaddingLeft(1).
	PaddingRight(1)

// ContextMenuItemDangerSelected is the selected state for a danger item.
var ContextMenuItemDangerSelected = lipgloss.NewStyle().
	Foreground(ColorBg).
	Background(ColorRed).
	PaddingLeft(1).
	PaddingRight(1).
	Bold(true)

// ContextMenuSep is a visual divider line within the context menu.
var ContextMenuSep = lipgloss.NewStyle().
	Foreground(ColorBorder).
	PaddingLeft(1).
	PaddingRight(1)

// ContextMenuShortcut renders the parenthetical shortcut hint (e.g., "(K)").
var ContextMenuShortcut = lipgloss.NewStyle().
	Foreground(ColorGray)

// SearchInput is the fuzzy search text input box at the top of the sidebar.
var SearchInput = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBlue).
	Foreground(ColorFg).
	Padding(0, 1)

// SearchPrompt is the "/" glyph prefix inside the search box.
var SearchPrompt = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

// SearchMatch highlights characters that matched the fuzzy query.
var SearchMatch = lipgloss.NewStyle().
	Foreground(ColorAmber).
	Bold(true)

// ---------------------------------------------------------------------------
// General focus border helpers
// ---------------------------------------------------------------------------

// FocusedBorder is a reusable style for any panel that currently has focus.
var FocusedBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBlue)

// UnfocusedBorder is a reusable style for any panel that does not have focus.
var UnfocusedBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBorder)

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

// EmptyState is the centered message shown when no sessions are found.
var EmptyState = lipgloss.NewStyle().
	Foreground(ColorGray).
	Italic(true)

// EmptyStateHint is the action hint below the empty state message.
var EmptyStateHint = lipgloss.NewStyle().
	Foreground(ColorBlue)
