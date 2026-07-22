// Package styles defines all Lipgloss visual styles for naviClaude.
// Tokyo Night color scheme matching the design mockups.
package styles

import "github.com/charmbracelet/lipgloss"

// ---------------------------------------------------------------------------
// Color palette (set by buildStyles; defaults to tokyo-night via init)
// ---------------------------------------------------------------------------
//
// These package-level Color* vars are assigned from the active Palette by
// buildStyles. They are declared with their tokyo-night default values purely
// for documentation/reference; init() immediately re-derives them (and every
// style below) from the default palette so there is a single source of truth.

var (
	ColorBg           = lipgloss.Color("#16161e") // terminal background (dark)
	ColorBgPanel      = lipgloss.Color("#1a1a2e") // sidebar / status bar panel bg
	ColorBgHover      = lipgloss.Color("#1e2240") // hover / key badge bg
	ColorFg           = lipgloss.Color("#c0caf5") // primary foreground
	ColorSelection    = lipgloss.Color("#2a3a5e") // selected item background (slightly brighter)
	ColorSelectionDim = lipgloss.Color("#243350") // dimmer selection for summary lines
	ColorBlue         = lipgloss.Color("#7aa2f7") // primary accent
	ColorGreen        = lipgloss.Color("#9ece6a") // active sessions, branch name
	ColorAmber        = lipgloss.Color("#e0af68") // waiting, group headers
	ColorRed          = lipgloss.Color("#f7768e") // danger / kill
	ColorGray         = lipgloss.Color("#565f89") // secondary text, times, descriptions
	ColorPurple       = lipgloss.Color("#bb9af7") // model, secondary accent
	ColorCyan         = lipgloss.Color("#7dcfff") // values, highlights
	ColorBorder       = lipgloss.Color("#333333") // borders, separators
	ColorDim          = lipgloss.Color("#444444") // closed sessions, faint elements
	ColorDimText      = lipgloss.Color("#787c99") // closed session names
)

// ---------------------------------------------------------------------------
// Style variables
// ---------------------------------------------------------------------------
//
// Every style var below is declared here (with its documentation) and is
// assigned exactly once, from the active palette, inside buildStyles. Do NOT
// add a default initializer here: that would re-introduce the duplicate
// definition that buildStyles exists to eliminate, and risk the two copies
// drifting apart.

// --- Sidebar styles ---

// SidebarPanel is the sidebar container with right border separator.
var SidebarPanel lipgloss.Style

// SidebarPanelFocused is the sidebar container when the session list has
// keyboard focus -- a thick blue right border marks the active pane's edge.
var SidebarPanelFocused lipgloss.Style

// RightSidebarPanel is the changed-files panel container (left border).
var RightSidebarPanel lipgloss.Style

// RightSidebarPanelFocused is the changed-files panel when it has focus.
var RightSidebarPanelFocused lipgloss.Style

// PaneTitleActive is the lit reverse-video title bar shown on the focused pane
// (blue background, bg-colored text). Derives from the palette so it follows
// every theme, including light ones (fg=Bg keeps contrast on the blue bar).
var PaneTitleActive lipgloss.Style

// PaneTitleInactive is the dim title shown on an unfocused pane.
var PaneTitleInactive lipgloss.Style

// SidebarItem is a normal (unselected) session entry in the sidebar.
var SidebarItem lipgloss.Style

// SelectionIndicator is a thin bar used as the left selection indicator.
// It is palette-independent, so it is a plain (non-derived) declaration.
var SelectionIndicator = lipgloss.Border{
	Left: "▎", // LEFT ONE QUARTER BLOCK -- thin solid bar
}

// SidebarItemSelected is the highlighted session entry with left blue bar.
var SidebarItemSelected lipgloss.Style

// SidebarGroupHeader is the tmux session name row -- amber per mockup.
var SidebarGroupHeader lipgloss.Style

// SidebarGroupCount renders the session count badge next to a group header.
var SidebarGroupCount lipgloss.Style

// SidebarProjectName is the project name within a session row.
var SidebarProjectName lipgloss.Style

// SidebarProjectNameSelected highlights the project name when selected.
var SidebarProjectNameSelected lipgloss.Style

// SidebarTime is the relative timestamp displayed next to a session.
var SidebarTime lipgloss.Style

// SidebarSummary is the truncated first-prompt summary line beneath a session.
var SidebarSummary lipgloss.Style

// SidebarSummarySelected is the summary line for the selected session.
// Matches SidebarItemSelected with left bar + selection background.
var SidebarSummarySelected lipgloss.Style

// Status icon characters -- single source of truth for all icon renderers.
const (
	IconActive  = "●" // filled circle
	IconWaiting = "◎" // bullseye
	IconIdle    = "○" // open circle
	IconClosed  = "◌" // dotted circle
)

// SidebarWaitingFlash is applied during the active->waiting transition flash.
var SidebarWaitingFlash lipgloss.Style

// --- Preview panel styles ---

// PreviewBorderUnfocused has NO border -- the separator is on the sidebar's
// right border now. This style is kept for backward compatibility but renders
// no border.
var PreviewBorderUnfocused lipgloss.Style

// PreviewBorderFocused also has NO border -- in passthrough mode the sidebar's
// right border turns blue instead.
var PreviewBorderFocused lipgloss.Style

// PreviewHeader is the bar at the top of the preview. No heavy background --
// just a bottom border line per the mockup design.
var PreviewHeader lipgloss.Style

// PreviewHeaderFocused is the header when the preview is in passthrough mode.
// Uses a blue bottom border to visually indicate the pane is focused.
var PreviewHeaderFocused lipgloss.Style

// PreviewHeaderLabel is a dimmer label within the header (separators, target).
var PreviewHeaderLabel lipgloss.Style

// PreviewHeaderValue is a value within the header (CPU, mem values).
var PreviewHeaderValue lipgloss.Style

// PreviewHeaderBranch highlights the git branch name.
var PreviewHeaderBranch lipgloss.Style

// PreviewHeaderAlert is a red warning badge in the header (e.g. "cache expired").
var PreviewHeaderAlert lipgloss.Style

// PreviewPassthroughBadge is the "PASSTHROUGH" indicator.
var PreviewPassthroughBadge lipgloss.Style

// StatusBadgeActive renders the "ACTIVE" text in the preview header.
var StatusBadgeActive lipgloss.Style

// StatusBadgeWaiting renders the "WAITING" text in the preview header.
var StatusBadgeWaiting lipgloss.Style

// StatusBadgeIdle renders the "IDLE" text in the preview header.
var StatusBadgeIdle lipgloss.Style

// PreviewContent is the base style for viewport body.
var PreviewContent lipgloss.Style

// PreviewSep is the separator character in the header.
var PreviewSep lipgloss.Style

// --- Status bar styles -- dark panel bg with badge-style keys ---

// StatusBar is the full-width bar at the bottom.
var StatusBar lipgloss.Style

// StatusBarKey is a single key hint label with badge background.
var StatusBarKey lipgloss.Style

// StatusBarDesc is the action description following a key hint.
var StatusBarDesc lipgloss.Style

// StatusBarSep is the separator between hint pairs.
var StatusBarSep lipgloss.Style

// StatusBarVersion is the version string on the right.
var StatusBarVersion lipgloss.Style

// StatusBarUpdate is the "update available" label shown beside the version.
var StatusBarUpdate lipgloss.Style

// --- Overlay / popup styles ---

var HelpBorder lipgloss.Style

var HelpTitle lipgloss.Style

var HelpKey lipgloss.Style

var HelpDesc lipgloss.Style

var ContextMenuBorder lipgloss.Style

var ContextMenuItem lipgloss.Style

var ContextMenuItemSelected lipgloss.Style

var ContextMenuItemDanger lipgloss.Style

var ContextMenuItemDangerSelected lipgloss.Style

var ContextMenuSep lipgloss.Style

var ContextMenuShortcut lipgloss.Style

var SearchInput lipgloss.Style

var SearchPrompt lipgloss.Style

var SearchMatch lipgloss.Style

// --- Title bar (top of screen) ---

// TitleBar is the full-width bar at the top of the application.
var TitleBar lipgloss.Style

// TitleBarName is the app name in the title bar.
var TitleBarName lipgloss.Style

// TitleBarDim is for secondary info in the title bar.
var TitleBarDim lipgloss.Style

// --- General ---

var FocusedBorder lipgloss.Style

var UnfocusedBorder lipgloss.Style

var EmptyState lipgloss.Style

var EmptyStateHint lipgloss.Style

// LoadingStyle is used for the loading indicator.
var LoadingStyle lipgloss.Style

// --- Conversation preview (closed sessions) ---

// ConversationUserLabel styles the "You" role header.
var ConversationUserLabel lipgloss.Style

// ConversationAssistantLabel styles the "Claude" role header.
var ConversationAssistantLabel lipgloss.Style

// ConversationUserText styles the user message body.
var ConversationUserText lipgloss.Style

// ConversationAssistantText styles the assistant message body.
var ConversationAssistantText lipgloss.Style

// ConversationSeparator styles the thin divider between turns.
var ConversationSeparator lipgloss.Style

// --- Detail overlay styles ---

var DetailBorder lipgloss.Style

var DetailTitle lipgloss.Style

var DetailLabel lipgloss.Style

var DetailValue lipgloss.Style

// --- Stats overlay styles ---

var StatsBorder lipgloss.Style

var StatsTitle lipgloss.Style

var StatsMetricValue lipgloss.Style

var StatsBar lipgloss.Style

var StatsBarAlt lipgloss.Style

var StatsFilterActive lipgloss.Style

var StatsFilterInactive lipgloss.Style

// defaultThemeKey is the palette used to populate styles at package init.
const defaultThemeKey = "tokyo-night"

// init populates every style variable from the default palette so the package
// is fully initialized before any rendering happens, even if ApplyTheme is
// never called.
func init() {
	buildStyles(Named(defaultThemeKey))
}

// buildStyles assigns every package-level Color* and style variable from the
// given palette. It is the single source of truth for style construction and
// is called from both init() (default palette) and ApplyTheme (selected theme).
//
// CONCURRENCY NOTE / known limitation: these are mutable package globals.
// ApplyTheme (which calls buildStyles) writes them, while Bubble Tea Cmd
// goroutines may read them concurrently when rendering. This is an accepted
// architectural limitation; the real fix is to thread the active theme through
// the model rather than mutating globals. Do NOT attempt to "fix" the race here
// -- only theme switching writes these, and it is rare and user-initiated.
func buildStyles(p Palette) {
	// Reassign color variables from the palette.
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

	// Build all style variables from the (now updated) colors.
	// Panel-fill styles set no Background so the app blends into the terminal's
	// own background instead of painting an opaque block behind the content.
	SidebarPanel = lipgloss.NewStyle().
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder)

	SidebarPanelFocused = lipgloss.NewStyle().
		BorderRight(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(ColorBlue)

	// RightSidebarPanel is the changed-files panel container. It uses a LEFT
	// border as its separator (mirror of SidebarPanel's right border).
	RightSidebarPanel = lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder)

	// RightSidebarPanelFocused is the changed-files panel when it has focus.
	RightSidebarPanelFocused = lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(ColorBlue)

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
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(ColorBlue)

	PaneTitleActive = lipgloss.NewStyle().
		Background(ColorBlue).
		Foreground(ColorBg).
		Bold(true)

	PaneTitleInactive = lipgloss.NewStyle().
		Foreground(ColorDimText).
		PaddingLeft(1)

	PreviewHeaderLabel = lipgloss.NewStyle().Foreground(ColorGray)
	PreviewHeaderValue = lipgloss.NewStyle().Foreground(ColorGray)
	PreviewHeaderBranch = lipgloss.NewStyle().Foreground(ColorGreen)
	PreviewHeaderAlert = lipgloss.NewStyle().Foreground(ColorRed).Bold(true)
	PreviewPassthroughBadge = lipgloss.NewStyle().Foreground(ColorGreen).Bold(true)
	StatusBadgeActive = lipgloss.NewStyle().Foreground(ColorGreen).Bold(true)
	StatusBadgeWaiting = lipgloss.NewStyle().Foreground(ColorAmber).Bold(true)
	StatusBadgeIdle = lipgloss.NewStyle().Foreground(ColorGray).Bold(true)
	PreviewContent = lipgloss.NewStyle().Foreground(ColorFg)
	PreviewSep = lipgloss.NewStyle().Foreground(ColorBorder)

	StatusBar = lipgloss.NewStyle().
		Foreground(ColorFg).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder)

	// StatusBarKey keeps a background: the key badge is a deliberate "keycap"
	// affordance, not part of the transparent bar fill.
	StatusBarKey = lipgloss.NewStyle().
		Foreground(ColorBlue).
		Background(ColorBgHover).
		Bold(true).
		PaddingLeft(1).
		PaddingRight(1)

	StatusBarDesc = lipgloss.NewStyle().
		Foreground(ColorGray)

	StatusBarSep = lipgloss.NewStyle().
		Foreground(ColorBorder)

	StatusBarVersion = lipgloss.NewStyle().
		Foreground(ColorGray)

	StatusBarUpdate = lipgloss.NewStyle().
		Foreground(ColorAmber).
		Bold(true)

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

// ApplyTheme applies the given palette, reassigning all color variables
// and rebuilding all style variables. Call once at startup before rendering.
//
// It delegates to buildStyles, which is the single source of truth shared with
// init(). See the concurrency note on buildStyles regarding global mutation.
func ApplyTheme(p Palette) {
	buildStyles(p)
}
