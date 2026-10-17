package claude

import (
	"github.com/thbits/naviClaude/internal/session"
	"regexp"
	"strings"
)

// classifyWindow is how many trailing non-empty lines are scanned; a prompt box
// (question, options, footer, input line) can sit well above the bottom.
const classifyWindow = 25

// selectionCursor matches ❯ directly before an option ("❯ 1.", "❯ Yes"), which
// tells a selection menu from the idle input box's ❯.
var selectionCursor = regexp.MustCompile(`(?i)❯\s*(\d+\.|yes\b|no\b)`)

// ClassifyPaneContent returns the strongest signal in captured pane content
// (ANSI is stripped internally); a waiting prompt beats the working footer.
func ClassifyPaneContent(content string) session.PaneSignal {
	// The window is bottom-to-top; every check below is order-independent.
	lines := lastNNonEmptyLines(content, classifyWindow)

	// Waiting takes priority: if a prompt is on screen, the session is blocked
	// on the user regardless of any stale "interrupt" footer above it.
	for _, line := range lines {
		if lineIsWaitingPrompt(line) {
			return session.SignalWaiting
		}
	}
	if isSelectionMenu(lines) {
		return session.SignalWaiting
	}

	// The "to interrupt" footer shows only while working; the spinner verb is
	// randomized and user-customizable, so it can't be the anchor.
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "to interrupt") {
			return session.SignalWorking
		}
	}

	return session.SignalNone
}

// isSelectionMenu reports a ❯ cursor option with a sibling option nearby, which
// catches free-form menus but not the boxed input prompt or a lone typed line.
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

// isCursorOptionLine reports a line starting with ❯ plus option text, outside a
// bordered box (the idle input prompt).
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
	return line[0] == ' ' || line[0] == '\t'
}

// lineIsWaitingPrompt reports whether an ANSI-stripped line shows a prompt. It
// matches Claude's English wording, so a copy change or locale can break it.
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
