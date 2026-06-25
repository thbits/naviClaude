package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestPreviewAtBottomExactLineCount is the regression guard for finding 11:
// the total line count is Count("\n")+1, so atBottom must be exact and not
// trigger one line early.
func TestPreviewAtBottomExactLineCount(t *testing.T) {
	p := NewPreview(20, 12) // viewport height = 12-2 = 10
	// 10 lines of content (9 newlines). With viewport height 10 the whole thing
	// fits, so YOffset 0 already means we are at the bottom.
	content := strings.Join([]string{
		"l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9",
	}, "\n")
	p.SetContent(content)

	// total lines = 10, height = 10, YOffset = 0 -> 0+10 >= 10 -> at bottom.
	if !p.atBottom() {
		t.Errorf("atBottom() = false at YOffset=0 with fitting content, want true")
	}

	// Now make content taller than the viewport so there is real scrollback.
	tall := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		tall = append(tall, "row")
	}
	p.SetContent(strings.Join(tall, "\n")) // 30 lines, 29 newlines

	// Scroll one line up from the bottom: must NOT be considered at bottom.
	p.userScrolled = true
	p.viewport.SetYOffset(30 - p.viewport.Height - 1) // one short of the true bottom
	if p.atBottom() {
		t.Errorf("atBottom() = true one line above true bottom, want false (resumes follow early)")
	}

	// At the true bottom offset it must report true.
	p.viewport.SetYOffset(30 - p.viewport.Height)
	if !p.atBottom() {
		t.Errorf("atBottom() = false at the true bottom, want true")
	}
}

// TestPreviewSetSizeTruncates is the regression guard for finding 12: a resize
// to a narrower width must re-run the per-line truncation so stale long lines
// do not overflow.
func TestPreviewSetSizeTruncates(t *testing.T) {
	p := NewPreview(80, 12)
	long := strings.Repeat("a", 200)
	p.SetContent(long)

	// Shrink: contentWidth becomes 20-2 = 18.
	p.SetSize(20, 12)

	rendered := p.viewport.View()
	for _, line := range strings.Split(rendered, "\n") {
		if w := ansi.StringWidth(line); w > p.viewport.Width {
			t.Fatalf("line width %d exceeds viewport width %d after resize: %q", w, p.viewport.Width, line)
		}
	}
}
