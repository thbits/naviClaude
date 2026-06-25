package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// PlaceOverlay renders the fg string centered on top of the bg string,
// preserving the background content outside the popup area.
// Both strings should be pre-rendered (may contain ANSI codes).
func PlaceOverlay(bg, fg string) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	if len(bgLines) == 0 || len(fgLines) == 0 {
		return bg
	}

	bgH := len(bgLines)
	fgH := len(fgLines)

	// Measure fg width from the widest line.
	fgW := 0
	for _, l := range fgLines {
		if w := lipgloss.Width(l); w > fgW {
			fgW = w
		}
	}

	// Measure bg width from the first line.
	bgW := lipgloss.Width(bgLines[0])

	startY := (bgH - fgH) / 2
	startX := (bgW - fgW) / 2
	if startX < 0 {
		startX = 0
	}
	if startY < 0 {
		startY = 0
	}

	for i, fgLine := range fgLines {
		bgRow := startY + i
		if bgRow < 0 || bgRow >= len(bgLines) {
			continue
		}

		bgLine := bgLines[bgRow]

		// Pad the bg line to bgW if it is shorter (e.g. blank lines).
		if lineW := lipgloss.Width(bgLine); lineW < bgW {
			bgLine += strings.Repeat(" ", bgW-lineW)
		}

		// Pad the fg line to fgW so the right portion of the bg is kept.
		if lineW := lipgloss.Width(fgLine); lineW < fgW {
			fgLine += strings.Repeat(" ", fgW-lineW)
		}

		left := ansi.Truncate(bgLine, startX, "")
		right := ansi.TruncateLeft(bgLine, startX+fgW, "")

		// A double-width rune straddling the popup's LEFT edge is dropped whole
		// by ansi.Truncate, so `left` comes back one column short and the popup
		// (and everything after it) shifts one column left. Pad the orphaned
		// half-cell with a space so the popup always begins exactly at column
		// startX. For ASCII, left is already exactly startX wide and no padding
		// is added, so ASCII output is byte-for-byte unchanged.
		if pad := startX - ansi.StringWidth(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}

		bgLines[bgRow] = left + fgLine + right
	}

	return strings.Join(bgLines, "\n")
}
