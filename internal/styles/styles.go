// Package styles defines all Lipgloss visual styles for naviClaude.
// Tokyo Night color scheme matching the design mockups.
package styles

import "github.com/charmbracelet/lipgloss"

// ---------------------------------------------------------------------------
// Tokyo Night color palette (defaults; overridden by ApplyTheme)
// ---------------------------------------------------------------------------

var (
	ColorBg        = lipgloss.Color("#16161e") // terminal background (dark)
	ColorBgPanel   = lipgloss.Color("#1a1a2e") // sidebar / status bar panel bg
	ColorBgHover   = lipgloss.Color("#1e2240") // hover / key badge bg
	ColorFg        = lipgloss.Color("#c0caf5") // primary foreground
	ColorSelection    = lipgloss.Color("#2a3a5e") // selected item background (slightly brighter)
	ColorSelectionDim = lipgloss.Color("#243350") // dimmer selection for summary lines
	ColorBlue         = lipgloss.Color("#7aa2f7") // primary accent
	ColorGreen     = lipgloss.Color("#9ece6a") // active sessions, branch name
	ColorAmber     = lipgloss.Color("#e0af68") // waiting, group headers
	ColorRed       = lipgloss.Color("#f7768e") // danger / kill
	ColorGray      = lipgloss.Color("#565f89") // secondary text, times, descriptions
	ColorPurple    = lipgloss.Color("#bb9af7") // model, secondary accent
	ColorCyan      = lipgloss.Color("#7dcfff") // values, highlights
	ColorBorder    = lipgloss.Color("#333333") // borders, separators
	ColorDim       = lipgloss.Color("#444444") // closed sessions, faint elements
	ColorDimText   = lipgloss.Color("#787c99") // closed session names
)

// ---------------------------------------------------------------------------
// Sidebar styles
// ---------------------------------------------------------------------------

// SidebarPanel is the sidebar container with right border separator.
var SidebarPanel = lipgloss.NewStyle().
	Background(ColorBgPanel).
	BorderRight(true).
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(ColorBorder)

// SidebarPanelFocused is the sidebar container when the preview is in
// passthrough mode -- blue right border to match mockup.
var SidebarPanelFocused = lipgloss.NewStyle().
	Background(ColorBgPanel).
	BorderRight(true).
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(ColorBlue)

// SidebarTitle is the "SESSIONS" header at the top of the sidebar.
var SidebarTitle = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true).
	PaddingLeft(1)

// SidebarTitleCount is the active session count next to the title.
var SidebarTitleCount = lipgloss.NewStyle().
	Foreground(ColorGray)

// SidebarItem is a normal (unselected) session entry in the sidebar.
var SidebarItem = lipgloss.NewStyle().
	Foreground(ColorFg).
	PaddingLeft(2)

// SelectionIndicator is a thin bar used as the left selection indicator.
var SelectionIndicator = lipgloss.Border{
	Left: "\u258e", // LEFT ONE QUARTER BLOCK -- thin solid bar
}

// SidebarItemSelected is the highlighted session entry with left blue bar.
var SidebarItemSelected = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Background(ColorSelection).
	Bold(true).
	PaddingLeft(1).
	BorderLeft(true).
	BorderStyle(SelectionIndicator).
	BorderForeground(ColorBlue)

// SidebarGroupHeader is the tmux session name row -- amber per mockup.
var SidebarGroupHeader = lipgloss.NewStyle().
	Foreground(ColorAmber).
	PaddingLeft(1)

// SidebarGroupCount renders the session count badge next to a group header.
var SidebarGroupCount = lipgloss.NewStyle().
	Foreground(ColorGray)

// SidebarProjectName is the project name within a session row.
var SidebarProjectName = lipgloss.NewStyle().
	Foreground(ColorFg)

// SidebarProjectNameSelected highlights the project name when selected.
var SidebarProjectNameSelected = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

// SidebarTime is the relative timestamp displayed next to a session.
var SidebarTime = lipgloss.NewStyle().
	Foreground(ColorGray)

// SidebarSummary is the truncated first-prompt summary line beneath a session.
var SidebarSummary = lipgloss.NewStyle().
	Foreground(ColorGray).
	PaddingLeft(4)

// SidebarSummarySelected is the summary line for the selected session.
// Matches SidebarItemSelected with left bar + selection background.
var SidebarSummarySelected = lipgloss.NewStyle().
	Foreground(ColorGray).
	Background(ColorSelectionDim).
	PaddingLeft(3).
	BorderLeft(true).
	BorderStyle(SelectionIndicator).
	BorderForeground(ColorPurple)

// Status icon characters -- single source of truth for all icon renderers.
const (
	IconActive  = "\u25cf" // filled circle
	IconWaiting = "\u25ce" // bullseye
	IconIdle    = "\u25cb" // open circle
	IconClosed  = "\u25cc" // dotted circle
)

// Status icon styles.

var StatusIconActive = lipgloss.NewStyle().
	Foreground(ColorGreen)

var StatusIconWaiting = lipgloss.NewStyle().
	Foreground(ColorAmber)

var StatusIconIdle = lipgloss.NewStyle().
	Foreground(ColorGray)

var StatusIconClosed = lipgloss.NewStyle().
	Foreground(ColorDim)

// SidebarWaitingFlash is applied during the active->waiting transition flash.
var SidebarWaitingFlash = lipgloss.NewStyle().
	Foreground(ColorBg).
	Background(ColorAmber).
	PaddingLeft(2).
	Bold(true)

// ---------------------------------------------------------------------------
// Preview panel styles
// ---------------------------------------------------------------------------

// PreviewBorderUnfocused has NO border -- the separator is on the sidebar's
// right border now. This style is kept for backward compatibility but renders
// no border.
var PreviewBorderUnfocused = lipgloss.NewStyle()

// PreviewBorderFocused also has NO border -- in passthrough mode the sidebar's
// right border turns blue instead.
var PreviewBorderFocused = lipgloss.NewStyle()

// PreviewHeader is the bar at the top of the preview. No heavy background --
// just a bottom border line per the mockup design.
var PreviewHeader = lipgloss.NewStyle().
	Foreground(ColorFg).
	PaddingLeft(1).
	PaddingRight(1).
	BorderBottom(true).
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(ColorBorder)

// PreviewHeaderFocused is the header when the preview is in passthrough mode.
// Uses a blue bottom border to visually indicate the pane is focused.
var PreviewHeaderFocused = lipgloss.NewStyle().
	Foreground(ColorFg).
	PaddingLeft(1).
	PaddingRight(1).
	BorderBottom(true).
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(ColorBlue)

// PreviewHeaderLabel is a dimmer label within the header (separators, target).
var PreviewHeaderLabel = lipgloss.NewStyle().
	Foreground(ColorGray)

// PreviewHeaderValue is a value within the header (CPU, mem values).
var PreviewHeaderValue = lipgloss.NewStyle().
	Foreground(ColorGray)

// PreviewHeaderBranch highlights the git branch name.
var PreviewHeaderBranch = lipgloss.NewStyle().
	Foreground(ColorGreen)

// PreviewPassthroughBadge is the "PASSTHROUGH" indicator.
var PreviewPassthroughBadge = lipgloss.NewStyle().
	Foreground(ColorGreen).
	Bold(true)

// StatusBadgeActive renders the "ACTIVE" text in the preview header.
var StatusBadgeActive = lipgloss.NewStyle().
	Foreground(ColorGreen).
	Bold(true)

// StatusBadgeWaiting renders the "WAITING" text in the preview header.
var StatusBadgeWaiting = lipgloss.NewStyle().
	Foreground(ColorAmber).
	Bold(true)

// StatusBadgeIdle renders the "IDLE" text in the preview header.
var StatusBadgeIdle = lipgloss.NewStyle().
	Foreground(ColorGray).
	Bold(true)

// PreviewContent is the base style for viewport body.
var PreviewContent = lipgloss.NewStyle().
	Foreground(ColorFg)

// PreviewSep is the separator character in the header.
var PreviewSep = lipgloss.NewStyle().
	Foreground(ColorBorder)

// ---------------------------------------------------------------------------
// Status bar styles -- dark panel bg with badge-style keys
// ---------------------------------------------------------------------------

// StatusBar is the full-width bar at the bottom.
var StatusBar = lipgloss.NewStyle().
	Background(ColorBgPanel).
	Foreground(ColorFg).
	BorderTop(true).
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(ColorBorder)

// StatusBarKey is a single key hint label with badge background.
var StatusBarKey = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Background(ColorBgHover).
	Bold(true).
	PaddingLeft(1).
	PaddingRight(1)

// StatusBarDesc is the action description following a key hint.
var StatusBarDesc = lipgloss.NewStyle().
	Foreground(ColorGray).
	Background(ColorBgPanel)

// StatusBarSep is the separator between hint pairs.
var StatusBarSep = lipgloss.NewStyle().
	Foreground(ColorBorder).
	Background(ColorBgPanel)

// StatusBarVersion is the version string on the right.
var StatusBarVersion = lipgloss.NewStyle().
	Foreground(ColorGray).
	Background(ColorBgPanel)

// ---------------------------------------------------------------------------
// Overlay / popup styles
// ---------------------------------------------------------------------------

var HelpBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorPurple).
	Background(ColorBgPanel).
	Padding(1, 2)

var HelpTitle = lipgloss.NewStyle().
	Foreground(ColorPurple).
	Bold(true).
	MarginBottom(1)

var HelpKey = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

var HelpDesc = lipgloss.NewStyle().
	Foreground(ColorFg)

var ContextMenuBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBorder).
	Background(ColorBgPanel)

var ContextMenuItem = lipgloss.NewStyle().
	Foreground(ColorFg).
	PaddingLeft(1).
	PaddingRight(1)

var ContextMenuItemSelected = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Background(ColorSelection).
	PaddingLeft(1).
	PaddingRight(1).
	Bold(true)

var ContextMenuItemDanger = lipgloss.NewStyle().
	Foreground(ColorRed).
	PaddingLeft(1).
	PaddingRight(1)

var ContextMenuItemDangerSelected = lipgloss.NewStyle().
	Foreground(ColorBg).
	Background(ColorRed).
	PaddingLeft(1).
	PaddingRight(1).
	Bold(true)

var ContextMenuSep = lipgloss.NewStyle().
	Foreground(ColorBorder).
	PaddingLeft(1).
	PaddingRight(1)

var ContextMenuShortcut = lipgloss.NewStyle().
	Foreground(ColorGray)

var SearchInput = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBlue).
	Foreground(ColorFg).
	Background(ColorBgHover).
	Padding(0, 1)

var SearchPrompt = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

var SearchMatch = lipgloss.NewStyle().
	Foreground(ColorAmber).
	Bold(true)

// ---------------------------------------------------------------------------
// Title bar (top of screen)
// ---------------------------------------------------------------------------

// TitleBar is the full-width bar at the top of the application.
var TitleBar = lipgloss.NewStyle().
	Background(ColorBgPanel).
	Foreground(ColorFg).
	BorderBottom(true).
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(ColorBorder)

// TitleBarName is the app name in the title bar.
var TitleBarName = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

// TitleBarDim is for secondary info in the title bar.
var TitleBarDim = lipgloss.NewStyle().
	Foreground(ColorGray)

// ---------------------------------------------------------------------------
// General
// ---------------------------------------------------------------------------

var FocusedBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBlue)

var UnfocusedBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBorder)

var EmptyState = lipgloss.NewStyle().
	Foreground(ColorGray).
	Italic(true)

var EmptyStateHint = lipgloss.NewStyle().
	Foreground(ColorBlue)

// LoadingStyle is used for the loading indicator.
var LoadingStyle = lipgloss.NewStyle().
	Foreground(ColorBlue)

// ---------------------------------------------------------------------------
// Conversation preview (closed sessions)
// ---------------------------------------------------------------------------

// ConversationUserLabel styles the "You" role header.
var ConversationUserLabel = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

// ConversationAssistantLabel styles the "Claude" role header.
var ConversationAssistantLabel = lipgloss.NewStyle().
	Foreground(ColorPurple).
	Bold(true)

// ConversationUserText styles the user message body.
var ConversationUserText = lipgloss.NewStyle().
	Foreground(ColorFg)

// ConversationAssistantText styles the assistant message body.
var ConversationAssistantText = lipgloss.NewStyle().
	Foreground(ColorDimText)

// ConversationSeparator styles the thin divider between turns.
var ConversationSeparator = lipgloss.NewStyle().
	Foreground(ColorBorder)

// ---------------------------------------------------------------------------
// Detail overlay styles
// ---------------------------------------------------------------------------

var DetailBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorPurple).
	Background(ColorBgPanel).
	Padding(1, 2)

var DetailTitle = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

var DetailLabel = lipgloss.NewStyle().
	Foreground(ColorGray)

var DetailValue = lipgloss.NewStyle().
	Foreground(ColorCyan)

// ---------------------------------------------------------------------------
// Stats overlay styles
// ---------------------------------------------------------------------------

var StatsBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorCyan).
	Background(ColorBgPanel).
	Padding(1, 2)

var StatsTitle = lipgloss.NewStyle().
	Foreground(ColorCyan).
	Bold(true)

var StatsMetricValue = lipgloss.NewStyle().
	Foreground(ColorGreen)

var StatsBar = lipgloss.NewStyle().
	Foreground(ColorBlue)

var StatsBarAlt = lipgloss.NewStyle().
	Foreground(ColorPurple)

var StatsFilterActive = lipgloss.NewStyle().
	Foreground(ColorCyan).
	Bold(true)

var StatsFilterInactive = lipgloss.NewStyle().
	Foreground(ColorGray)

// ApplyTheme applies the given palette, reassigning all color variables
// and rebuilding all style variables. Call once at startup before rendering.
func ApplyTheme(p Palette) {
	// Reassign color variables.
	ColorBg = p.Bg
	ColorBgPanel = p.BgPanel
	ColorBgHover = p.BgHover
	ColorFg = p.Fg
	ColorSelection = p.Selection
	ColorSelectionDim = p.SelectionDim
	ColorBlue = p.Blue
	ColorGreen = p.Green
	ColorAmber = p.Amber
	ColorRed = p.Red
	ColorGray = p.Gray
	ColorPurple = p.Purple
	ColorCyan = p.Cyan
	ColorBorder = p.Border
	ColorDim = p.Dim
	ColorDimText = p.DimText

	// Rebuild all style variables to use the new colors.
	SidebarPanel = lipgloss.NewStyle().
		Background(ColorBgPanel).
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder)

	SidebarPanelFocused = lipgloss.NewStyle().
		Background(ColorBgPanel).
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorBlue)

	SidebarTitle = lipgloss.NewStyle().
		Foreground(ColorBlue).
		Bold(true).
		PaddingLeft(1)

	SidebarTitleCount = lipgloss.NewStyle().
		Foreground(ColorGray)

	SidebarItem = lipgloss.NewStyle().
		Foreground(ColorFg).
		PaddingLeft(2)

	SidebarItemSelected = lipgloss.NewStyle().
		Foreground(ColorBlue).
		Background(ColorSelection).
		Bold(true).
		PaddingLeft(1).
		BorderLeft(true).
		BorderStyle(SelectionIndicator).
		BorderForeground(ColorBlue)

	SidebarGroupHeader = lipgloss.NewStyle().
		Foreground(ColorAmber).
		PaddingLeft(1)

	SidebarGroupCount = lipgloss.NewStyle().
		Foreground(ColorGray)

	SidebarProjectName = lipgloss.NewStyle().
		Foreground(ColorFg)

	SidebarProjectNameSelected = lipgloss.NewStyle().
		Foreground(ColorBlue).
		Bold(true)

	SidebarTime = lipgloss.NewStyle().
		Foreground(ColorGray)

	SidebarSummary = lipgloss.NewStyle().
		Foreground(ColorGray).
		PaddingLeft(4)

	SidebarSummarySelected = lipgloss.NewStyle().
		Foreground(ColorGray).
		Background(ColorSelectionDim).
		PaddingLeft(3).
		BorderLeft(true).
		BorderStyle(SelectionIndicator).
		BorderForeground(ColorPurple)

	StatusIconActive = lipgloss.NewStyle().Foreground(ColorGreen)
	StatusIconWaiting = lipgloss.NewStyle().Foreground(ColorAmber)
	StatusIconIdle = lipgloss.NewStyle().Foreground(ColorGray)
	StatusIconClosed = lipgloss.NewStyle().Foreground(ColorDim)

	SidebarWaitingFlash = lipgloss.NewStyle().
		Foreground(ColorBg).
		Background(ColorAmber).
		PaddingLeft(2).
		Bold(true)

	PreviewBorderUnfocused = lipgloss.NewStyle()
	PreviewBorderFocused = lipgloss.NewStyle()

	PreviewHeader = lipgloss.NewStyle().
		Foreground(ColorFg).
		PaddingLeft(1).
		PaddingRight(1).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder)

	PreviewHeaderFocused = lipgloss.NewStyle().
		Foreground(ColorFg).
		PaddingLeft(1).
		PaddingRight(1).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorBlue)

	PreviewHeaderLabel = lipgloss.NewStyle().Foreground(ColorGray)
	PreviewHeaderValue = lipgloss.NewStyle().Foreground(ColorGray)
	PreviewHeaderBranch = lipgloss.NewStyle().Foreground(ColorGreen)
	PreviewPassthroughBadge = lipgloss.NewStyle().Foreground(ColorGreen).Bold(true)
	StatusBadgeActive = lipgloss.NewStyle().Foreground(ColorGreen).Bold(true)
	StatusBadgeWaiting = lipgloss.NewStyle().Foreground(ColorAmber).Bold(true)
	StatusBadgeIdle = lipgloss.NewStyle().Foreground(ColorGray).Bold(true)
	PreviewContent = lipgloss.NewStyle().Foreground(ColorFg)
	PreviewSep = lipgloss.NewStyle().Foreground(ColorBorder)

	StatusBar = lipgloss.NewStyle().
		Background(ColorBgPanel).
		Foreground(ColorFg).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder)

	StatusBarKey = lipgloss.NewStyle().
		Foreground(ColorBlue).
		Background(ColorBgHover).
		Bold(true).
		PaddingLeft(1).
		PaddingRight(1)

	StatusBarDesc = lipgloss.NewStyle().
		Foreground(ColorGray).
		Background(ColorBgPanel)

	StatusBarSep = lipgloss.NewStyle().
		Foreground(ColorBorder).
		Background(ColorBgPanel)

	StatusBarVersion = lipgloss.NewStyle().
		Foreground(ColorGray).
		Background(ColorBgPanel)

	HelpBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPurple).
		Background(ColorBgPanel).
		Padding(1, 2)

	HelpTitle = lipgloss.NewStyle().
		Foreground(ColorPurple).
		Bold(true).
		MarginBottom(1)

	HelpKey = lipgloss.NewStyle().Foreground(ColorBlue).Bold(true)
	HelpDesc = lipgloss.NewStyle().Foreground(ColorFg)

	ContextMenuBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Background(ColorBgPanel)

	ContextMenuItem = lipgloss.NewStyle().
		Foreground(ColorFg).
		PaddingLeft(1).
		PaddingRight(1)

	ContextMenuItemSelected = lipgloss.NewStyle().
		Foreground(ColorBlue).
		Background(ColorSelection).
		PaddingLeft(1).
		PaddingRight(1).
		Bold(true)

	ContextMenuItemDanger = lipgloss.NewStyle().
		Foreground(ColorRed).
		PaddingLeft(1).
		PaddingRight(1)

	ContextMenuItemDangerSelected = lipgloss.NewStyle().
		Foreground(ColorBg).
		Background(ColorRed).
		PaddingLeft(1).
		PaddingRight(1).
		Bold(true)

	ContextMenuSep = lipgloss.NewStyle().
		Foreground(ColorBorder).
		PaddingLeft(1).
		PaddingRight(1)

	ContextMenuShortcut = lipgloss.NewStyle().Foreground(ColorGray)

	SearchInput = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBlue).
		Foreground(ColorFg).
		Background(ColorBgHover).
		Padding(0, 1)

	SearchPrompt = lipgloss.NewStyle().Foreground(ColorBlue).Bold(true)
	SearchMatch = lipgloss.NewStyle().Foreground(ColorAmber).Bold(true)

	TitleBar = lipgloss.NewStyle().
		Background(ColorBgPanel).
		Foreground(ColorFg).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder)

	TitleBarName = lipgloss.NewStyle().Foreground(ColorBlue).Bold(true)
	TitleBarDim = lipgloss.NewStyle().Foreground(ColorGray)

	FocusedBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBlue)

	UnfocusedBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder)

	EmptyState = lipgloss.NewStyle().Foreground(ColorGray).Italic(true)
	EmptyStateHint = lipgloss.NewStyle().Foreground(ColorBlue)
	LoadingStyle = lipgloss.NewStyle().Foreground(ColorBlue)

	ConversationUserLabel = lipgloss.NewStyle().Foreground(ColorBlue).Bold(true)
	ConversationAssistantLabel = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	ConversationUserText = lipgloss.NewStyle().Foreground(ColorFg)
	ConversationAssistantText = lipgloss.NewStyle().Foreground(ColorDimText)
	ConversationSeparator = lipgloss.NewStyle().Foreground(ColorBorder)

	DetailBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPurple).
		Background(ColorBgPanel).
		Padding(1, 2)

	DetailTitle = lipgloss.NewStyle().Foreground(ColorBlue).Bold(true)
	DetailLabel = lipgloss.NewStyle().Foreground(ColorGray)
	DetailValue = lipgloss.NewStyle().Foreground(ColorCyan)

	StatsBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorCyan).
		Background(ColorBgPanel).
		Padding(1, 2)

	StatsTitle = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	StatsMetricValue = lipgloss.NewStyle().Foreground(ColorGreen)
	StatsBar = lipgloss.NewStyle().Foreground(ColorBlue)
	StatsBarAlt = lipgloss.NewStyle().Foreground(ColorPurple)
	StatsFilterActive = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	StatsFilterInactive = lipgloss.NewStyle().Foreground(ColorGray)
}
