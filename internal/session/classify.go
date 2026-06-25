package session

import (
	"regexp"
	"strings"
)

// classifyWindow is how many trailing non-empty lines we scan. A prompt box is
// question + options + footer + the input line, so it can sit well above the
// bottom of the screen -- 6 lines (the old window) was too small.
const classifyWindow = 25

// selectionCursor matches the ❯ menu cursor followed by an option: "❯ 1.",
// "❯ Yes", "❯ No". This distinguishes an active selection menu (waiting) from
// the idle input box, which also shows ❯ but never directly before an option.
var selectionCursor = regexp.MustCompile(`(?i)❯\s*(\d+\.|yes\b|no\b)`)

// PaneSignal is the input/activity signal extracted purely from a pane's
// captured content, independent of process or CPU state. The StatusTracker
// combines it with CPU and transcript signals to produce a final SessionStatus.
type PaneSignal int

const (
	// SignalNone means the content shows neither a waiting prompt nor the
	// "interrupt" footer; the session is idle, or its activity must be inferred
	// from CPU/transcript signals.
	SignalNone PaneSignal = iota
	// SignalWaiting means an interactive prompt requiring user input is visible.
	SignalWaiting
	// SignalWorking means Claude is generating or running tools, indicated by
	// the "esc to interrupt" footer it shows only while busy.
	SignalWorking
)

// ClassifyPaneContent inspects captured pane content (ANSI sequences are
// stripped internally) and returns the strongest signal it can find. A waiting
// prompt always takes priority over the working footer.
func ClassifyPaneContent(content string) PaneSignal {
	// lastNNonEmptyLines yields the window bottom-to-top. The order does not
	// matter here: the waiting/working scans below return on any match (so they
	// are order-independent), and isSelectionMenu's adjacency check (±2 lines) is
	// symmetric, so reversing to top-to-bottom screen order would not change any
	// result. Pass it through directly.
	lines := lastNNonEmptyLines(content, classifyWindow)

	// Waiting takes priority: if a prompt is on screen, the session is blocked
	// on the user regardless of any stale "interrupt" footer above it.
	for _, line := range lines {
		if lineIsWaitingPrompt(line) {
			return SignalWaiting
		}
	}
	if isSelectionMenu(lines) {
		return SignalWaiting
	}

	// Working: the "to interrupt" footer is shown only while generating or
	// running tools. We anchor on "to interrupt" rather than the spinner verb,
	// which is randomized ("Cogitating…", "Forging…") and user-customizable.
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "to interrupt") {
			return SignalWorking
		}
	}

	return SignalNone
}

// isSelectionMenu reports whether the window contains an interactive selection
// menu awaiting a choice: the ❯ cursor before option text, with an adjacent
// sibling option line. This catches free-form menus (Claude's question prompt)
// whose options are arbitrary text, while excluding the idle input box (the ❯
// there sits inside a bordered box) and a lone typed-in input line (no sibling).
func isSelectionMenu(lines []string) bool {
	for i, line := range lines {
		if !isCursorOptionLine(line) {
			continue
		}
		for j := i - 2; j <= i+2; j++ {
			if j < 0 || j >= len(lines) || j == i {
				continue
			}
			if isOptionLine(lines[j]) {
				return true
			}
		}
	}
	return false
}

// isCursorOptionLine reports whether a line is a menu selection cursor: it
// starts (after leading whitespace) with ❯ followed by option text, and is not
// part of a bordered box (the idle input prompt).
func isCursorOptionLine(line string) bool {
	if strings.ContainsRune(line, '│') {
		return false
	}
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "❯") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "❯"))
	return rest != ""
}

// isOptionLine reports whether a line looks like an unselected menu option: an
// indented (or cursor-marked) non-empty line that is not a box border.
func isOptionLine(line string) bool {
	if strings.ContainsRune(line, '│') {
		return false
	}
	if strings.TrimSpace(line) == "" {
		return false
	}
	// Cursor-marked sibling (a second ❯ is unusual but harmless to accept).
	if isCursorOptionLine(line) {
		return true
	}
	// Indented option text: leading whitespace before content.
	return line[0] == ' ' || line[0] == '\t'
}

// lineIsWaitingPrompt reports whether a single ANSI-stripped line indicates an
// interactive prompt awaiting user input.
//
// NOTE: these are brittle heuristics. They match hard-coded English substrings
// (e.g. "do you want to", "no, and tell claude what to do") and rely on the
// blank-line-stripped adjacency of menu options. They are tightly coupled to
// the exact wording and layout Claude Code uses for its prompts, so a CLI copy
// change or a non-English locale can silently break Waiting detection. The
// matching logic is intentionally left unchanged here; revisit it if Claude's
// prompt wording changes.
func lineIsWaitingPrompt(line string) bool {
	lower := strings.ToLower(line)

	switch {
	// Bracketed confirmation: [Y/n], [y/N], [n/y].
	case strings.Contains(lower, "[y/n]"), strings.Contains(lower, "[n/y]"):
		return true
	// Question phrasings used by permission / plan / edit prompts.
	case strings.Contains(lower, "do you want to"),
		strings.Contains(lower, "would you like to proceed"),
		strings.Contains(lower, "do you trust"):
		return true
	// The decline option that accompanies every permission prompt.
	case strings.Contains(lower, "no, and tell claude what to do"),
		strings.Contains(lower, "what should claude do instead"):
		return true
	// Explicit selection menus.
	case strings.Contains(lower, "enter to select"),
		strings.Contains(lower, "enter to confirm"),
		strings.Contains(lower, "press enter to"):
		return true
	// Legacy permission line.
	case strings.Contains(line, "Allow?"):
		return true
	// The ❯ selection cursor directly before a menu option.
	case selectionCursor.MatchString(line):
		return true
	}
	return false
}
