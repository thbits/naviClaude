package preview

import (
	"strings"

	"github.com/tomhalo/naviclaude/internal/session"
)

// StatusDetector detects the status of a Claude Code session by analyzing
// the captured pane content. It only performs prompt detection -- it does NOT
// try to detect "active" vs "idle" via content comparison, because spinners,
// status bars, timestamps, and cursor blink cause constant false-positive
// content changes that result in status flickering.
//
// Active status comes from the session Detector (process is running).
// This detector only transitions to Waiting when a prompt is visible.
type StatusDetector struct{}

// NewStatusDetector creates a StatusDetector.
func NewStatusDetector() *StatusDetector {
	return &StatusDetector{}
}

// DetectFromContent inspects captured pane content and returns Waiting if a
// known prompt pattern is detected, or StatusActive otherwise. The caller
// should only apply Waiting status updates to avoid overriding the detector's
// authoritative Active status.
func (d *StatusDetector) DetectFromContent(target, content string) session.SessionStatus {
	if matchesInputPrompt(content) {
		return session.StatusWaiting
	}
	return session.StatusActive
}

// Reset is a no-op (kept for interface compatibility).
func (d *StatusDetector) Reset(target string) {}

// matchesInputPrompt reports whether the captured pane content ends with a
// line that matches a known Claude Code input prompt:
//   - The Unicode prompt character (U+276F) used by Claude Code (❯)
//   - A [Y/n] or [y/N] confirmation prompt
//   - An "Allow?" permission request
func matchesInputPrompt(content string) bool {
	last := lastNonEmptyLine(content)
	if last == "" {
		return false
	}

	// Claude Code prompt character: ❯ (U+276F), optionally followed by a space
	// and cursor.
	if strings.HasPrefix(last, "\u276f") {
		return true
	}

	// Case-insensitive confirmation prompts: [Y/n] or [y/N]
	lower := strings.ToLower(last)
	if strings.Contains(lower, "[y/n]") || strings.Contains(lower, "[n/y]") {
		return true
	}

	// Permission request
	if strings.Contains(last, "Allow?") {
		return true
	}

	return false
}

// lastNonEmptyLine returns the last line in s that contains at least one
// non-whitespace character after stripping ANSI escape sequences.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		stripped := stripANSI(lines[i])
		if strings.TrimSpace(stripped) != "" {
			return stripped
		}
	}
	return ""
}

// stripANSI removes ANSI/VT100 escape sequences from a string using a simple
// state-machine parser. This is intentionally lightweight; for full ANSI
// processing the charmbracelet/x/ansi package is available, but a basic
// strip is sufficient for prompt detection.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// CSI sequence: skip until a byte in 0x40-0x7E
			i += 2
			for i < len(s) {
				if s[i] >= 0x40 && s[i] <= 0x7e {
					i++
					break
				}
				i++
			}
			continue
		}
		if c == '\x1b' {
			// Other escape: skip next byte
			i += 2
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}
