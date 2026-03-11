package preview

import (
	"strings"

	"github.com/tomhalo/naviclaude/internal/session"
)

// activeCycleThreshold is the number of consecutive identical captures
// required before a pane is considered stable (not active).
const activeCycleThreshold = 2

// StatusDetector detects the status of a Claude Code session by analyzing
// consecutive captures of the pane content.
type StatusDetector struct {
	prevCaptures map[string]string // previous capture per tmux target
	stableCycles map[string]int    // consecutive identical capture count per target
}

// NewStatusDetector creates a StatusDetector.
func NewStatusDetector() *StatusDetector {
	return &StatusDetector{
		prevCaptures: make(map[string]string),
		stableCycles: make(map[string]int),
	}
}

// DetectFromContent analyzes already-captured pane content, compares it with
// the previous capture for this target, updates the stable-cycle counter, and
// returns the inferred SessionStatus. This avoids a redundant tmux capture-pane
// call since the caller already has the content.
func (d *StatusDetector) DetectFromContent(target, content string) session.SessionStatus {
	prev, seen := d.prevCaptures[target]
	if !seen || content != prev {
		// Content changed (or first observation): reset stable counter.
		d.prevCaptures[target] = content
		d.stableCycles[target] = 0
		return session.StatusActive
	}

	// Content unchanged: increment stable counter.
	d.stableCycles[target]++

	if d.stableCycles[target] < activeCycleThreshold {
		return session.StatusActive
	}

	// Pane is stable. Determine waiting vs idle by inspecting the last
	// non-empty line of the captured content.
	if matchesInputPrompt(content) {
		return session.StatusWaiting
	}
	return session.StatusIdle
}

// Reset clears the stored state for a target. Call this when a session is
// closed or its pane is recycled.
func (d *StatusDetector) Reset(target string) {
	delete(d.prevCaptures, target)
	delete(d.stableCycles, target)
}

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
