package ui

import (
	"github.com/charmbracelet/x/ansi"
)

// ellipsis is the single-cell horizontal ellipsis used when truncating text.
const ellipsis = "…"

// truncateDisplay shortens s so its terminal display width does not exceed
// maxWidth, appending an ellipsis when characters are dropped. It is aware of
// multibyte runes, wide (e.g. CJK) graphemes, and ANSI escape sequences, so it
// never splits a rune mid-byte the way a naive s[:n] byte slice would.
//
// Guard cases:
//   - maxWidth <= 0 returns "" (there is no room for any content).
//   - When maxWidth is too small to fit the ellipsis alongside any content,
//     ansi.Truncate yields just the ellipsis (or "" if even that won't fit).
func truncateDisplay(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, ellipsis)
}

// truncateDisplayLeft shortens s from the LEFT so its terminal display width
// does not exceed maxWidth, prepending an ellipsis when characters are dropped.
// Use it for file paths, where the tail (basename and extension) is the most
// useful part to keep visible. Display-width and ANSI aware like truncateDisplay.
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

// labelColumnWidth returns the display width of the widest label, used to size
// an aligned label column. It is display-width aware so multibyte/wide labels
// align correctly.
func labelColumnWidth(labels []string) int {
	max := 0
	for _, l := range labels {
		if w := ansi.StringWidth(l); w > max {
			max = w
		}
	}
	return max
}

// alignedLabelPad returns the spaces to insert between a rendered label and its
// value so that values line up in a column. colWidth is the width of the widest
// label (see labelColumnWidth); label is the raw (unstyled) label text; extra is
// the number of additional spaces after the column (the gutter between label and
// value). The result is never negative.
func alignedLabelPad(label string, colWidth, extra int) int {
	pad := colWidth - ansi.StringWidth(label) + extra
	if pad < 0 {
		pad = 0
	}
	return pad
}
