package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/styles"
)

// StatusBarModel renders the bottom bar with contextual keybinding hints.
type StatusBarModel struct {
	width           int
	mode            string
	version         string
	updateAvailable bool         // a newer release exists on GitHub
	listHints       []statusHint // configurable list-mode hints
	errText         string
	notice          bool // errText is a success notice, not an error
	confirmKill     bool // the kill prompt is up, whatever the mode
}

// statusHint is one key hint. key is the label replayed when the hint is
// clicked, so it must stay parseable as a key name ("Enter", "Ctrl+]").
type statusHint struct {
	key    string
	desc   string
	label  string // keycap text when it differs from key (e.g. "⏎/e")
	pinned bool   // kept however narrow the bar gets
	inert  bool   // names no single key (e.g. "j/k"), so a click must do nothing
}

// keycap is the text drawn in the hint's key badge.
func (h statusHint) keycap() string {
	if h.label != "" {
		return h.label
	}
	if strings.EqualFold(h.key, "enter") {
		return "⏎"
	}
	return h.key
}

// NewStatusBar creates a StatusBarModel with the given width and version string.
func NewStatusBar(width int, version string) StatusBarModel {
	return StatusBarModel{
		width:   width,
		mode:    "list",
		version: version,
	}
}

// StatusHintInput is the public type for setting status bar hints from outside the package.
type StatusHintInput struct {
	Key  string
	Desc string
}

// SetKeyHints updates the list-mode hints from the config-derived KeyMap,
// ordered by priority. The last one (help) stays visible at any width.
func (m *StatusBarModel) SetKeyHints(hints []StatusHintInput) {
	m.listHints = make([]statusHint, len(hints))
	for i, h := range hints {
		m.listHints[i] = statusHint{key: h.Key, desc: h.Desc, pinned: i == len(hints)-1}
	}
}

// SetMode sets the current mode which determines which hints are displayed.
func (m *StatusBarModel) SetMode(mode string) {
	m.mode = mode
}

// SetConfirmKill swaps in the kill prompt's hints while the confirmation is up.
func (m *StatusBarModel) SetConfirmKill(v bool) {
	m.confirmKill = v
}

// SetUpdateAvailable toggles the "update available" label next to the version.
func (m *StatusBarModel) SetUpdateAvailable(v bool) {
	m.updateAvailable = v
}

// SetSize updates the status bar width.
func (m *StatusBarModel) SetSize(w int) {
	m.width = w
}

// SetError shows a transient error message in the status bar.
func (m *StatusBarModel) SetError(msg string) {
	m.errText, m.notice = msg, false
}

// SetNotice shows a transient success message in the slot SetError uses.
func (m *StatusBarModel) SetNotice(msg string) {
	m.errText, m.notice = msg, true
}

// ClearError removes the transient error message.
func (m *StatusBarModel) ClearError() {
	m.errText = ""
}

// Init satisfies the tea.Model interface.
func (m StatusBarModel) Init() tea.Cmd {
	return nil
}

// Update is a no-op; the status bar does not handle input.
func (m StatusBarModel) Update(msg tea.Msg) (StatusBarModel, tea.Cmd) {
	return m, nil
}

// View renders the status bar.
func (m StatusBarModel) View() string {
	if m.errText != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e")).Bold(true)
		if m.notice {
			errStyle = lipgloss.NewStyle().Foreground(styles.ColorGreen).Bold(true)
		}
		bar := errStyle.Render(" " + m.errText)
		return m.renderBar(bar)
	}

	left, _ := m.layoutHints()
	right := styles.StatusBarVersion.Render(m.version)
	if m.updateAvailable {
		withLabel := right + styles.StatusBarVersion.Render("  ") + styles.StatusBarUpdate.Render("update available")
		// Only show the label when it fits: an overflowing bar wraps to a
		// second line and shifts the whole app layout up a row.
		if lipgloss.Width(left)+1+lipgloss.Width(withLabel) <= m.width {
			right = withLabel
		}
	}

	// Compute gap between left hints and right-aligned version.
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	gap := m.width - leftWidth - rightWidth
	if gap < 1 {
		gap = 1
	}

	return m.renderBar(left + strings.Repeat(" ", gap) + right)
}

// hintSpan is the column range [start, end) a hint occupies on the bar.
type hintSpan struct {
	key        string
	inert      bool
	start, end int
}

func renderHint(h statusHint) string {
	return styles.StatusBarKey.Render(h.keycap()) + " " + styles.StatusBarDesc.Render(h.desc)
}

func hintSep() string { return styles.StatusBarSep.Render(" · ") }

// hintsWidth is the rendered width of hints joined by separators.
func hintsWidth(hints []statusHint) int {
	w := 0
	for i, h := range hints {
		if i > 0 {
			w += lipgloss.Width(hintSep())
		}
		w += lipgloss.Width(renderHint(h))
	}
	return w
}

// fitHints drops the lowest-priority unpinned hints until the rest fit in
// budget columns, so a narrow bar loses whole hints instead of cutting words.
func fitHints(hints []statusHint, budget int) []statusHint {
	out := append([]statusHint(nil), hints...)
	for hintsWidth(out) > budget {
		drop := -1
		for i := len(out) - 1; i >= 0; i-- {
			if !out[i].pinned {
				drop = i
				break
			}
		}
		if drop < 0 {
			break // only pinned hints left; renderBar truncates as a last resort
		}
		out = append(out[:drop], out[drop+1:]...)
	}
	return out
}

// fittedHints is the current mode's hints that fit beside the version.
func (m StatusBarModel) fittedHints() []statusHint {
	budget := m.width - lipgloss.Width(styles.StatusBarVersion.Render(m.version)) - 1
	return fitHints(m.hintsForMode(), budget)
}

// layoutHints renders the hints that fit beside the version and records each
// one's column span, so View and HintAt share one layout.
func (m StatusBarModel) layoutHints() (string, []hintSpan) {
	hints := m.fittedHints()
	sep := hintSep()
	sepW := lipgloss.Width(sep)

	var b strings.Builder
	spans := make([]hintSpan, 0, len(hints))
	col := 0
	for i, h := range hints {
		part := renderHint(h)
		w := lipgloss.Width(part)
		spans = append(spans, hintSpan{key: h.key, inert: h.inert, start: col, end: col + w})
		b.WriteString(part)
		col += w
		if i < len(hints)-1 {
			b.WriteString(sep)
			col += sepW
		}
	}
	return b.String(), spans
}

// HintAt returns the key label of the replayable hint drawn at column x of the
// hint line. Nothing is clickable while an error replaces the hints.
func (m StatusBarModel) HintAt(x int) (string, bool) {
	s, ok := m.spanAt(x)
	if !ok || s.inert {
		return "", false
	}
	return s.key, true
}

// OnHint reports whether column x is on any hint, replayable or not, so a click
// there is never taken for a click outside an open overlay.
func (m StatusBarModel) OnHint(x int) bool {
	_, ok := m.spanAt(x)
	return ok
}

func (m StatusBarModel) spanAt(x int) (hintSpan, bool) {
	if m.errText != "" || x < 0 || x >= m.width {
		return hintSpan{}, false
	}
	_, spans := m.layoutHints()
	for _, s := range spans {
		if x >= s.start && x < s.end {
			return s, true
		}
	}
	return hintSpan{}, false
}

// renderBar applies the StatusBar style, truncating overflow because a wrapped,
// taller status bar would break the app's content-height math.
func (m StatusBarModel) renderBar(bar string) string {
	return styles.StatusBar.Width(m.width).Render(ansi.Truncate(bar, m.width, ""))
}

// hintsForMode returns the current mode's hints, most useful first.
func (m StatusBarModel) hintsForMode() []statusHint {
	if m.confirmKill {
		return []statusHint{{key: "y", desc: "kill"}, {key: "any key", desc: "cancel"}}
	}
	switch m.mode {
	case "passthrough":
		// Tab is forwarded to the agent here, so only Shift+Tab leaves (to the list).
		return []statusHint{
			{key: "Shift+Tab", desc: "back"},
			{key: "Ctrl+]", desc: "list"},
			{key: "Ctrl+f", label: "Ctrl+F", desc: "jump"},
		}
	case "changed files":
		return []statusHint{
			{key: "Enter", label: "⏎/e", desc: "open in editor"},
			{key: "Esc", desc: "back"},
		}
	case "search":
		return []statusHint{
			{key: "Esc", desc: "cancel"},
			{key: "Enter", desc: "select"},
			{key: "Ctrl+f", label: "Ctrl+F", desc: "jump"},
			{key: "PgUp", label: "PgUp/Dn", desc: "scroll", inert: true},
		}
	case "name input", "rename":
		return []statusHint{{key: "Enter", desc: "confirm"}, {key: "Esc", desc: "cancel"}}
	case "dir picker", "resume picker":
		// The popup lists its own keys; a click outside it cancels.
		return []statusHint{{key: "Esc", desc: "cancel"}}
	case "detail", "help":
		return []statusHint{{key: "any key", desc: "close"}}
	case "stats":
		return []statusHint{{key: "Tab", desc: "cycle filter"}, {key: "any key", desc: "close"}}
	case "themepicker":
		return []statusHint{{key: "j/k", desc: "navigate", inert: true}, {key: "Enter", desc: "apply"}, {key: "Esc", desc: "cancel"}}
	case "contextmenu":
		return []statusHint{{key: "j/k", desc: "navigate", inert: true}, {key: "Enter", desc: "select"}, {key: "Esc", desc: "cancel"}}
	default: // "list"
		if len(m.listHints) > 0 {
			return m.listHints
		}
		return []statusHint{
			{key: "Enter", desc: "open"},
			{key: "f", desc: "jump"},
			{key: "n", desc: "new"},
			{key: "/", desc: "search"},
			{key: "F", desc: "files"},
			{key: "K", desc: "kill"},
			{key: "?", desc: "more", pinned: true},
		}
	}
}
