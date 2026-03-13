package preview

import (
	"strings"

	"github.com/thbits/naviClaude/internal/session"
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

// matchesInputPrompt reports whether the captured pane content contains a
// known Claude Code interactive prompt within the last several non-empty lines.
//
// We do NOT detect the ❯ (U+276F) character because it is always visible in
// every Claude session as part of the standard TUI layout. Instead we look for
// specific interactive patterns that only appear when Claude needs user input.
func matchesInputPrompt(content string) bool {
	lines := lastNNonEmptyLines(content, 6)
	for _, line := range lines {
		lower := strings.ToLower(line)

		// Confirmation prompts: [Y/n] or [y/N]
		if strings.Contains(lower, "[y/n]") || strings.Contains(lower, "[n/y]") {
			return true
		}

		// Permission request
		if strings.Contains(line, "Allow?") {
			return true
		}

		// Interactive selection menus (onboarding, trust dialog, etc.)
		if strings.Contains(lower, "enter to select") ||
			strings.Contains(lower, "enter to confirm") {
			return true
		}

		// Interrupted session waiting for new direction
		if strings.Contains(line, "What should Claude do instead?") {
			return true
		}
	}

	return false
}

// lastNNonEmptyLines returns up to n non-empty lines from the end of the string
// after stripping ANSI escape sequences.
func lastNNonEmptyLines(s string, n int) []string {
	lines := strings.Split(s, "\n")
	var result []string
	for i := len(lines) - 1; i >= 0 && len(result) < n; i-- {
		stripped := stripANSI(lines[i])
		if strings.TrimSpace(stripped) != "" {
			result = append(result, stripped)
		}
	}
	return result
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
