package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestStatusBar_UpdateLabel(t *testing.T) {
	sb := NewStatusBar(120, "1.2.3")

	if strings.Contains(sb.View(), "update available") {
		t.Fatal("label should be absent before SetUpdateAvailable(true)")
	}

	sb.SetUpdateAvailable(true)
	if !strings.Contains(sb.View(), "update available") {
		t.Error("label should be present after SetUpdateAvailable(true)")
	}
	if !strings.Contains(sb.View(), "1.2.3") {
		t.Error("version should still be present alongside the label")
	}
}

// The rendered bar must never grow taller than its normal height: overflow
// wraps to an extra line, and app.go's contentHeight math then shifts the
// whole layout up a row. On narrow widths the update label is dropped (and,
// as a last resort, the bar truncated) rather than wrapped.
func TestStatusBar_NoOverflowOnNarrowWidth(t *testing.T) {
	wide := NewStatusBar(200, "1.2.3")
	wantHeight := lipgloss.Height(wide.View())

	for _, width := range []int{80, 60, 40, 20} {
		sb := NewStatusBar(width, "1.2.3")
		sb.SetUpdateAvailable(true)
		view := sb.View()

		if h := lipgloss.Height(view); h != wantHeight {
			t.Errorf("width %d: status bar height = %d, want %d (bar wrapped)", width, h, wantHeight)
		}
		for _, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line width = %d, want <= %d", width, w, width)
			}
		}
	}

	// The error path shares the bar and must not wrap either.
	sb := NewStatusBar(20, "1.2.3")
	sb.SetError("some very long error message that cannot possibly fit")
	if h := lipgloss.Height(sb.View()); h != wantHeight {
		t.Errorf("error path: status bar height = %d, want %d (bar wrapped)", h, wantHeight)
	}
}
