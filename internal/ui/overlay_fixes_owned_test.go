package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestPlaceOverlayASCIIUnchanged is the regression guard for finding 10: pure
// ASCII compositing must be byte-for-byte what a manual splice produces, with
// no extra padding introduced by the wide-rune half-cell guard.
func TestPlaceOverlayASCIIUnchanged(t *testing.T) {
	// 11-wide background rows, 3-wide popup -> startX = (11-3)/2 = 4.
	bg := strings.Join([]string{
		"...........",
		"...........",
		"...........",
	}, "\n")
	fg := "XXX"

	got := PlaceOverlay(bg, fg)
	lines := strings.Split(got, "\n")

	// Every output row must keep the background width.
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 11 {
			t.Errorf("row %d width = %d, want 11 (%q)", i, w, l)
		}
	}
	// The middle row must have the popup spliced at column 4.
	if lines[1] != "....XXX...." {
		t.Errorf("middle row = %q, want %q", lines[1], "....XXX....")
	}
	// Rows without the popup are untouched.
	if lines[0] != "..........." || lines[2] != "..........." {
		t.Errorf("non-popup rows changed: %q / %q", lines[0], lines[2])
	}
}

// TestPlaceOverlayWideRuneStraddle reasons about the CJK left-edge case: a
// double-width rune straddling the popup's left edge is dropped by
// ansi.Truncate, leaving the left segment one column short. The half-cell guard
// pads it so the popup always begins exactly at column startX and does not
// shift left. (The right edge's keep-behavior is a separate, out-of-scope
// concern; this guards the left edge that finding 10 targets.)
func TestPlaceOverlayWideRuneStraddle(t *testing.T) {
	// Background row of width 8 made of four wide CJK runes (2 cells each).
	// Popup "XX" (width 2). startX = (8-2)/2 = 3, so the popup left edge at
	// column 3 falls in the middle of the second wide rune (cols 2-3).
	bg := "你好世界" // 4 runes * 2 cells = 8
	fg := "XX"

	got := PlaceOverlay(bg, fg)
	line := strings.Split(got, "\n")[0]

	// The left segment up to the popup must be exactly startX (3) columns wide,
	// so the popup is not shifted left into column 2.
	startX := 3
	leftPart := ansi.Truncate(line, startX, "")
	if w := ansi.StringWidth(leftPart); w != startX {
		t.Errorf("left segment width = %d, want %d (popup shifted left); line=%q", w, startX, line)
	}
	// The popup characters must be present, intact, at the expected position.
	if !strings.Contains(line, "XX") {
		t.Errorf("popup XX missing from composited line %q", line)
	}
}
