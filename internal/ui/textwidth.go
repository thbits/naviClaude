package ui

import (
	"github.com/charmbracelet/x/ansi"
)

// ellipsis is the single-cell horizontal ellipsis used when truncating text.
const ellipsis = "…"

// truncateDisplay shortens s to maxWidth display columns with a trailing ellipsis,
// safe for multibyte, wide and ANSI-styled text, unlike a byte slice.
func truncateDisplay(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, ellipsis)
}

// truncateDisplayLeft is truncateDisplay from the LEFT, for paths where the
// basename is the part worth keeping visible.
func truncateDisplayLeft(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w <= maxWidth {
		return s
	}
	// Remove enough leading cells to leave room for the ellipsis + the tail:
	// result width == 1 (ellipsis) + (w - n) == maxWidth.
	return ansi.TruncateLeft(s, w-maxWidth+1, ellipsis)
}

// labelColumnWidth returns the display width of the widest label, for sizing an
// aligned label column.
func labelColumnWidth(labels []string) int {
	max := 0
	for _, l := range labels {
		if w := ansi.StringWidth(l); w > max {
			max = w
		}
	}
	return max
}

// alignedLabelPad returns the non-negative space count after the raw label so values
// line up at colWidth (see labelColumnWidth) plus an extra gutter.
func alignedLabelPad(label string, colWidth, extra int) int {
	pad := colWidth - ansi.StringWidth(label) + extra
	if pad < 0 {
		pad = 0
	}
	return pad
}
