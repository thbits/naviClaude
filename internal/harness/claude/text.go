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

// stripANSIBasic removes ANSI/VT100 escape sequences from a string. It delegates
// to ansi.Strip (the same robust implementation used by internal/preview), which
// correctly handles CSI (ESC [ ...) and OSC (ESC ] ... BEL/ST) sequences that the
// previous hand-rolled scanner mishandled. Real Claude pane output only ever
// contains CSI and OSC sequences, so for those inputs this matches the preview
// path exactly.
//
// The one carve-out preserves a long-standing contract: a bare ESC that is NOT a
// CSI/OSC introducer (e.g. "a\x1bXb") is treated as a 2-byte drop (ESC + the next
// byte), matching the legacy scanner. ansi.Strip would instead treat ESC X as an
// SOS string-control opener and swallow everything up to its terminator, which is
// surprising for arbitrary mid-text bytes that are not really escape sequences.
func stripANSIBasic(s string) string {
	// Fast path: when every ESC introduces a CSI or OSC sequence (the only kinds
	// real Claude output contains), ansi.Strip handles the whole string. Only fall
	// back to the legacy byte scanner when a non-CSI/non-OSC ESC is present.
	if hasBareEscape(s) {
		return stripBareEscapeScanner(s)
	}
	return ansi.Strip(s)
}

// hasBareEscape reports whether s contains an ESC that is not immediately
// followed by '[' (CSI) or ']' (OSC) -- i.e. a sequence ansi.Strip would treat
// differently from the legacy 2-byte-drop behavior.
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

// stripBareEscapeScanner reproduces the legacy scanner: CSI sequences are
// consumed up to their final byte (0x40-0x7e), and any other ESC drops itself
// plus the following byte. Used only when hasBareEscape is true.
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
