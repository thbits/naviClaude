package styles

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// styleFingerprint captures the visually-significant properties of a style:
// foreground/background colors, bold/italic, padding, margin, and the border
// edges plus their colors. Unlike Style.Render -- which emits no escape codes
// in a non-TTY test environment and is therefore blind to color and bold --
// these getters return the configured values directly, so a regression in any
// theme-relevant attribute changes the fingerprint. This is exactly what the
// buildStyles refactor must preserve.
func styleFingerprint(s lipgloss.Style) string {
	return fmt.Sprintf(
		"fg=%v bg=%v bold=%v italic=%v pad=%d/%d/%d/%d mb=%d border=%v "+
			"edges=%v/%v/%v/%v bfg=%v/%v/%v/%v",
		s.GetForeground(), s.GetBackground(), s.GetBold(), s.GetItalic(),
		s.GetPaddingTop(), s.GetPaddingRight(), s.GetPaddingBottom(), s.GetPaddingLeft(),
		s.GetMarginBottom(), s.GetBorderStyle(),
		s.GetBorderTop(), s.GetBorderRight(), s.GetBorderBottom(), s.GetBorderLeft(),
		s.GetBorderTopForeground(), s.GetBorderRightForeground(),
		s.GetBorderBottomForeground(), s.GetBorderLeftForeground(),
	)
}

// TestBuildStylesDefaultMatchesTokyoNight verifies that, after package init,
// every package-level style equals an independently-constructed copy built from
// the tokyo-night palette using the same lipgloss chain that existed before the
// buildStyles extraction. If buildStyles drifted from the original definitions,
// these comparisons fail.
func TestBuildStylesDefaultMatchesTokyoNight(t *testing.T) {
	p := Named("tokyo-night")
	// Pin the resolved default palette to the exact tokyo-night hex values.
	if string(p.Bg) != "#16161e" || string(p.Fg) != "#c0caf5" {
		t.Fatalf("tokyo-night palette changed unexpectedly: Bg=%q Fg=%q", p.Bg, p.Fg)
	}

	// Reference colors (the historic default initializers).
	var (
		cBg           = lipgloss.Color("#16161e")
		cBgPanel      = lipgloss.Color("#1a1a2e")
		cBgHover      = lipgloss.Color("#1e2240")
		cFg           = lipgloss.Color("#c0caf5")
		cSelection    = lipgloss.Color("#2a3a5e")
		cSelectionDim = lipgloss.Color("#243350")
		cBlue         = lipgloss.Color("#7aa2f7")
		cGreen        = lipgloss.Color("#9ece6a")
		cAmber        = lipgloss.Color("#e0af68")
		cRed          = lipgloss.Color("#f7768e")
		cGray         = lipgloss.Color("#565f89")
		cPurple       = lipgloss.Color("#bb9af7")
		cCyan         = lipgloss.Color("#7dcfff")
		cBorder       = lipgloss.Color("#333333")
		cDimText      = lipgloss.Color("#787c99")
	)
	// ColorDim (#444444) is still a package var consumed by callers (sidebar),
	// but no styles-package global style derives from it now that the unused
	// StatusIcon* styles are removed, so it is intentionally not referenced here.

	// Independently reconstruct the expected default styles, mirroring the
	// original top-of-file definitions exactly.
	want := map[string]lipgloss.Style{
		// Panel-fill styles are intentionally transparent so the app blends into
		// the terminal's own background instead of painting an opaque dark block.
		"SidebarPanel": lipgloss.NewStyle().BorderRight(true).
			BorderStyle(lipgloss.NormalBorder()).BorderForeground(cBorder),
		"SidebarPanelFocused": lipgloss.NewStyle().BorderRight(true).
			BorderStyle(lipgloss.ThickBorder()).BorderForeground(cBlue),
		"SidebarTitle":      lipgloss.NewStyle().Foreground(cBlue).Bold(true).PaddingLeft(1),
		"SidebarTitleCount": lipgloss.NewStyle().Foreground(cGray),
		"PaneTitleActive":   lipgloss.NewStyle().Background(cBlue).Foreground(cBg).Bold(true),
		"PaneTitleInactive": lipgloss.NewStyle().Foreground(cDimText).PaddingLeft(1),
		"SidebarItem":       lipgloss.NewStyle().Foreground(cFg).PaddingLeft(2),
		"SidebarItemSelected": lipgloss.NewStyle().Foreground(cBlue).Background(cSelection).
			Bold(true).PaddingLeft(1).BorderLeft(true).BorderStyle(SelectionIndicator).
			BorderForeground(cBlue),
		"SidebarGroupHeader":         lipgloss.NewStyle().Foreground(cAmber).PaddingLeft(1),
		"SidebarGroupCount":          lipgloss.NewStyle().Foreground(cGray),
		"SidebarProjectName":         lipgloss.NewStyle().Foreground(cFg),
		"SidebarProjectNameSelected": lipgloss.NewStyle().Foreground(cBlue).Bold(true),
		"SidebarTime":                lipgloss.NewStyle().Foreground(cGray),
		"SidebarSummary":             lipgloss.NewStyle().Foreground(cGray).PaddingLeft(4),
		"SidebarSummarySelected": lipgloss.NewStyle().Foreground(cGray).Background(cSelectionDim).
			PaddingLeft(3).BorderLeft(true).BorderStyle(SelectionIndicator).BorderForeground(cPurple),
		"SidebarWaitingFlash": lipgloss.NewStyle().Foreground(cBg).Background(cAmber).
			PaddingLeft(2).Bold(true),
		"PreviewBorderUnfocused": lipgloss.NewStyle(),
		"PreviewBorderFocused":   lipgloss.NewStyle(),
		"PreviewHeader": lipgloss.NewStyle().Foreground(cFg).PaddingLeft(1).PaddingRight(1).
			BorderBottom(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(cBorder),
		"PreviewHeaderFocused": lipgloss.NewStyle().Foreground(cFg).PaddingLeft(1).PaddingRight(1).
			BorderBottom(true).BorderStyle(lipgloss.ThickBorder()).BorderForeground(cBlue),
		"PreviewHeaderLabel":      lipgloss.NewStyle().Foreground(cGray),
		"PreviewHeaderValue":      lipgloss.NewStyle().Foreground(cGray),
		"PreviewHeaderBranch":     lipgloss.NewStyle().Foreground(cGreen),
		"PreviewHeaderAlert":      lipgloss.NewStyle().Foreground(cRed).Bold(true),
		"PreviewPassthroughBadge": lipgloss.NewStyle().Foreground(cGreen).Bold(true),
		"StatusBadgeActive":       lipgloss.NewStyle().Foreground(cGreen).Bold(true),
		"StatusBadgeWaiting":      lipgloss.NewStyle().Foreground(cAmber).Bold(true),
		"StatusBadgeIdle":         lipgloss.NewStyle().Foreground(cGray).Bold(true),
		"PreviewContent":          lipgloss.NewStyle().Foreground(cFg),
		"PreviewSep":              lipgloss.NewStyle().Foreground(cBorder),
		"StatusBar": lipgloss.NewStyle().Foreground(cFg).BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).BorderForeground(cBorder),
		"StatusBarKey": lipgloss.NewStyle().Foreground(cBlue).Background(cBgHover).Bold(true).
			PaddingLeft(1).PaddingRight(1),
		"StatusBarDesc":    lipgloss.NewStyle().Foreground(cGray),
		"StatusBarSep":     lipgloss.NewStyle().Foreground(cBorder),
		"StatusBarVersion": lipgloss.NewStyle().Foreground(cGray),
		"HelpBorder": lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(cPurple).Background(cBgPanel).Padding(1, 2),
		"HelpTitle":       lipgloss.NewStyle().Foreground(cPurple).Bold(true).MarginBottom(1),
		"HelpKey":         lipgloss.NewStyle().Foreground(cBlue).Bold(true),
		"HelpDesc":        lipgloss.NewStyle().Foreground(cFg),
		"ContextMenuItem": lipgloss.NewStyle().Foreground(cFg).PaddingLeft(1).PaddingRight(1),
		"ContextMenuItemSelected": lipgloss.NewStyle().Foreground(cBlue).Background(cSelection).
			PaddingLeft(1).PaddingRight(1).Bold(true),
		"ContextMenuItemDanger": lipgloss.NewStyle().Foreground(cRed).PaddingLeft(1).PaddingRight(1),
		"ContextMenuItemDangerSelected": lipgloss.NewStyle().Foreground(cBg).Background(cRed).
			PaddingLeft(1).PaddingRight(1).Bold(true),
		"ContextMenuSep":      lipgloss.NewStyle().Foreground(cBorder).PaddingLeft(1).PaddingRight(1),
		"ContextMenuShortcut": lipgloss.NewStyle().Foreground(cGray),
		"SearchInput": lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBlue).
			Foreground(cFg).Background(cBgHover).Padding(0, 1),
		"SearchPrompt": lipgloss.NewStyle().Foreground(cBlue).Bold(true),
		"SearchMatch":  lipgloss.NewStyle().Foreground(cAmber).Bold(true),
		"TitleBar": lipgloss.NewStyle().Foreground(cFg).BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).BorderForeground(cBorder),
		"TitleBarName":               lipgloss.NewStyle().Foreground(cBlue).Bold(true),
		"TitleBarDim":                lipgloss.NewStyle().Foreground(cGray),
		"FocusedBorder":              lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBlue),
		"UnfocusedBorder":            lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder),
		"EmptyState":                 lipgloss.NewStyle().Foreground(cGray).Italic(true),
		"EmptyStateHint":             lipgloss.NewStyle().Foreground(cBlue),
		"LoadingStyle":               lipgloss.NewStyle().Foreground(cBlue),
		"ConversationUserLabel":      lipgloss.NewStyle().Foreground(cBlue).Bold(true),
		"ConversationAssistantLabel": lipgloss.NewStyle().Foreground(cPurple).Bold(true),
		"ConversationUserText":       lipgloss.NewStyle().Foreground(cFg),
		"ConversationAssistantText":  lipgloss.NewStyle().Foreground(cDimText),
		"ConversationSeparator":      lipgloss.NewStyle().Foreground(cBorder),
		"DetailBorder": lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cPurple).
			Background(cBgPanel).Padding(1, 2),
		"DetailTitle": lipgloss.NewStyle().Foreground(cBlue).Bold(true),
		"DetailLabel": lipgloss.NewStyle().Foreground(cGray),
		"DetailValue": lipgloss.NewStyle().Foreground(cCyan),
		"StatsBorder": lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cCyan).
			Background(cBgPanel).Padding(1, 2),
		"StatsTitle":          lipgloss.NewStyle().Foreground(cCyan).Bold(true),
		"StatsMetricValue":    lipgloss.NewStyle().Foreground(cGreen),
		"StatsBar":            lipgloss.NewStyle().Foreground(cBlue),
		"StatsBarAlt":         lipgloss.NewStyle().Foreground(cPurple),
		"StatsFilterActive":   lipgloss.NewStyle().Foreground(cCyan).Bold(true),
		"StatsFilterInactive": lipgloss.NewStyle().Foreground(cGray),
	}

	// ContextMenuBorder built separately to keep gofmt happy.
	want["ContextMenuBorder"] = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).Background(cBgPanel)

	got := map[string]lipgloss.Style{
		"SidebarPanel":                  SidebarPanel,
		"SidebarPanelFocused":           SidebarPanelFocused,
		"SidebarTitle":                  SidebarTitle,
		"SidebarTitleCount":             SidebarTitleCount,
		"PaneTitleActive":               PaneTitleActive,
		"PaneTitleInactive":             PaneTitleInactive,
		"SidebarItem":                   SidebarItem,
		"SidebarItemSelected":           SidebarItemSelected,
		"SidebarGroupHeader":            SidebarGroupHeader,
		"SidebarGroupCount":             SidebarGroupCount,
		"SidebarProjectName":            SidebarProjectName,
		"SidebarProjectNameSelected":    SidebarProjectNameSelected,
		"SidebarTime":                   SidebarTime,
		"SidebarSummary":                SidebarSummary,
		"SidebarSummarySelected":        SidebarSummarySelected,
		"SidebarWaitingFlash":           SidebarWaitingFlash,
		"PreviewBorderUnfocused":        PreviewBorderUnfocused,
		"PreviewBorderFocused":          PreviewBorderFocused,
		"PreviewHeader":                 PreviewHeader,
		"PreviewHeaderFocused":          PreviewHeaderFocused,
		"PreviewHeaderLabel":            PreviewHeaderLabel,
		"PreviewHeaderValue":            PreviewHeaderValue,
		"PreviewHeaderBranch":           PreviewHeaderBranch,
		"PreviewHeaderAlert":            PreviewHeaderAlert,
		"PreviewPassthroughBadge":       PreviewPassthroughBadge,
		"StatusBadgeActive":             StatusBadgeActive,
		"StatusBadgeWaiting":            StatusBadgeWaiting,
		"StatusBadgeIdle":               StatusBadgeIdle,
		"PreviewContent":                PreviewContent,
		"PreviewSep":                    PreviewSep,
		"StatusBar":                     StatusBar,
		"StatusBarKey":                  StatusBarKey,
		"StatusBarDesc":                 StatusBarDesc,
		"StatusBarSep":                  StatusBarSep,
		"StatusBarVersion":              StatusBarVersion,
		"HelpBorder":                    HelpBorder,
		"HelpTitle":                     HelpTitle,
		"HelpKey":                       HelpKey,
		"HelpDesc":                      HelpDesc,
		"ContextMenuBorder":             ContextMenuBorder,
		"ContextMenuItem":               ContextMenuItem,
		"ContextMenuItemSelected":       ContextMenuItemSelected,
		"ContextMenuItemDanger":         ContextMenuItemDanger,
		"ContextMenuItemDangerSelected": ContextMenuItemDangerSelected,
		"ContextMenuSep":                ContextMenuSep,
		"ContextMenuShortcut":           ContextMenuShortcut,
		"SearchInput":                   SearchInput,
		"SearchPrompt":                  SearchPrompt,
		"SearchMatch":                   SearchMatch,
		"TitleBar":                      TitleBar,
		"TitleBarName":                  TitleBarName,
		"TitleBarDim":                   TitleBarDim,
		"FocusedBorder":                 FocusedBorder,
		"UnfocusedBorder":               UnfocusedBorder,
		"EmptyState":                    EmptyState,
		"EmptyStateHint":                EmptyStateHint,
		"LoadingStyle":                  LoadingStyle,
		"ConversationUserLabel":         ConversationUserLabel,
		"ConversationAssistantLabel":    ConversationAssistantLabel,
		"ConversationUserText":          ConversationUserText,
		"ConversationAssistantText":     ConversationAssistantText,
		"ConversationSeparator":         ConversationSeparator,
		"DetailBorder":                  DetailBorder,
		"DetailTitle":                   DetailTitle,
		"DetailLabel":                   DetailLabel,
		"DetailValue":                   DetailValue,
		"StatsBorder":                   StatsBorder,
		"StatsTitle":                    StatsTitle,
		"StatsMetricValue":              StatsMetricValue,
		"StatsBar":                      StatsBar,
		"StatsBarAlt":                   StatsBarAlt,
		"StatsFilterActive":             StatsFilterActive,
		"StatsFilterInactive":           StatsFilterInactive,
	}

	if len(want) != len(got) {
		t.Fatalf("style count mismatch: want %d, got %d", len(want), len(got))
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("missing package style %q", name)
			continue
		}
		if styleFingerprint(w) != styleFingerprint(g) {
			t.Errorf("style %q diverged from original default definition\n want=%s\n  got=%s",
				name, styleFingerprint(w), styleFingerprint(g))
		}
	}
}

// TestApplyThemeReassignsColors verifies ApplyTheme swaps the package color vars
// and that re-applying tokyo-night restores the defaults (so test order/state
// from other tests does not leak).
func TestApplyThemeReassignsColors(t *testing.T) {
	t.Cleanup(func() { ApplyTheme(Named(fallbackThemeKey)) })

	ApplyTheme(Named("dracula"))
	if string(ColorBg) != "#282a36" {
		t.Errorf("ApplyTheme(dracula) did not set ColorBg: got %q", ColorBg)
	}
	// StatusBar is transparent (no fill); assert on a style that still carries a
	// rebuilt background -- the key badge, which keeps ColorBgHover.
	if string(StatusBarKey.GetBackground().(lipgloss.Color)) != "#343746" {
		t.Errorf("ApplyTheme(dracula) did not rebuild StatusBarKey bg: got %q",
			StatusBarKey.GetBackground())
	}

	ApplyTheme(Named(fallbackThemeKey))
	if string(ColorBg) != "#16161e" {
		t.Errorf("re-applying default did not restore ColorBg: got %q", ColorBg)
	}
}

// TestNamedFallback verifies an unknown theme falls back to a non-zero palette,
// guarding finding #3 (the const-keyed fallback must resolve to a real entry).
func TestNamedFallback(t *testing.T) {
	p := Named("does-not-exist")
	if p.Name == "" || string(p.Bg) == "" {
		t.Fatalf("fallback returned zero Palette: %+v", p)
	}
	if p.Name != Themes[fallbackThemeKey].Name {
		t.Errorf("fallback theme = %q, want %q", p.Name, Themes[fallbackThemeKey].Name)
	}
}
