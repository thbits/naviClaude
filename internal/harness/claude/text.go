package claude

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// lastNNonEmptyLines returns up to n non-empty lines from the end of the string
// after stripping ANSI escape sequences.
func lastNNonEmptyLines(s string, n int) []string {
	lines := strings.Split(s, "\n")
	var result []string
	for i := len(lines) - 1; i >= 0 && len(result) < n; i-- {
		stripped := stripANSIBasic(lines[i])
		if strings.TrimSpace(stripped) != "" {
			result = append(result, stripped)
		}
	}
	return result
}

// stripANSIBasic strips ANSI like the preview does, except a bare non-CSI/OSC ESC
// drops only itself and the next byte, where ansi.Strip would swallow text as SOS.
func stripANSIBasic(s string) string {
	// Real Claude output has only CSI/OSC, so the scanner path is rare.
	if hasBareEscape(s) {
		return stripBareEscapeScanner(s)
	}
	return ansi.Strip(s)
}

// hasBareEscape reports an ESC not followed by '[' (CSI) or ']' (OSC).
func hasBareEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			if i+1 >= len(s) {
				return true
			}
			if s[i+1] != '[' && s[i+1] != ']' {
				return true
			}
		}
	}
	return false
}

// stripBareEscapeScanner consumes CSI sequences to their final byte (0x40-0x7e)
// and drops any other ESC plus the byte after it.
func stripBareEscapeScanner(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
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
			i += 2
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}
