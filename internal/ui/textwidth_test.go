package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTruncateDisplay(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		maxWidth int
		want     string
	}{
		{"empty input", "", 10, ""},
		{"fits exactly", "hello", 5, "hello"},
		{"shorter than max", "hi", 10, "hi"},
		{"ascii truncated with ellipsis", "hello world", 5, "hell" + ellipsis},
		{"maxWidth zero returns empty", "hello", 0, ""},
		{"maxWidth negative returns empty", "hello", -3, ""},
		// Multibyte: each Cyrillic rune is one display cell. Truncating to 3
		// must not split a rune mid-byte and must leave room for the ellipsis.
		{"multibyte truncated", "абвгд", 3, "аб" + ellipsis},
		{"multibyte fits", "абв", 3, "абв"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDisplay(tt.in, tt.maxWidth)
			if got != tt.want {
				t.Errorf("truncateDisplay(%q, %d) = %q, want %q", tt.in, tt.maxWidth, got, tt.want)
			}
			// Result must never exceed the requested display width.
			if w := ansi.StringWidth(got); tt.maxWidth > 0 && w > tt.maxWidth {
				t.Errorf("truncateDisplay(%q, %d) width = %d, exceeds max", tt.in, tt.maxWidth, w)
			}
		})
	}
}

func TestTruncateDisplayNoPanicSmallWidths(t *testing.T) {
	// The old byte-slice truncate panicked for maxLen<=0; the new one must not,
	// for any width including the awkward 1-cell case where only the ellipsis
	// (or nothing) fits.
	for w := -2; w <= 3; w++ {
		_ = truncateDisplay("multibyte:абвгд", w)
	}
}

func TestLabelColumnWidth(t *testing.T) {
	got := labelColumnWidth([]string{"a", "bbb", "cc"})
	if got != 3 {
		t.Errorf("labelColumnWidth = %d, want 3", got)
	}
	if got := labelColumnWidth(nil); got != 0 {
		t.Errorf("labelColumnWidth(nil) = %d, want 0", got)
	}
}

func TestAlignedLabelPad(t *testing.T) {
	// colWidth 5, label "ab" (width 2), extra 2 -> 5-2+2 = 5
	if got := alignedLabelPad("ab", 5, 2); got != 5 {
		t.Errorf("alignedLabelPad = %d, want 5", got)
	}
	// Never negative even when the label is wider than the column.
	if got := alignedLabelPad("toolong", 3, 0); got != 0 {
		t.Errorf("alignedLabelPad negative case = %d, want 0", got)
	}
}
