package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// PlaceOverlay renders the pre-rendered fg centered over bg, keeping bg content
// outside the popup area.
func PlaceOverlay(bg, fg string) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	if len(bgLines) == 0 || len(fgLines) == 0 {
		return bg
	}

	// Measure bg width from the first line.
	bgW := lipgloss.Width(bgLines[0])
	startX, startY, _, _ := OverlayRect(bgW, len(bgLines), fg)
	return PlaceOverlayAt(bg, fg, startX, startY)
}

// PlaceOverlayAt draws fg over bg with its top-left corner at column x, row y,
// keeping the bg content on either side of the popup.
func PlaceOverlayAt(bg, fg string, x, y int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	if len(bgLines) == 0 || len(fgLines) == 0 {
		return bg
	}
	bgW := lipgloss.Width(bgLines[0])
	startX, startY := x, y
	fgW := 0
	for _, l := range fgLines {
		if lw := lipgloss.Width(l); lw > fgW {
			fgW = lw
		}
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

		// ansi.Truncate drops a wide rune straddling startX whole, leaving left one
		// column short; pad it so the popup always starts exactly at startX.
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
