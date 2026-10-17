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

	// Measure bg width from the first line.
	bgW := lipgloss.Width(bgLines[0])
	startX, startY, fgW, _ := OverlayRect(bgW, len(bgLines), fg)

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

// OverlayRect returns where PlaceOverlay draws fg over a bgW x bgH background,
// so mouse hit-tests agree with the composited popup.
func OverlayRect(bgW, bgH int, fg string) (x, y, w, h int) {
	fgLines := strings.Split(fg, "\n")
	for _, l := range fgLines {
		if lw := lipgloss.Width(l); lw > w {
			w = lw
		}
	}
	h = len(fgLines)
	x = (bgW - w) / 2
	y = (bgH - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y, w, h
}

// InRect reports whether (px, py) falls inside the x, y, w, h rectangle.
func InRect(px, py, x, y, w, h int) bool {
	return px >= x && px < x+w && py >= y && py < y+h
}
