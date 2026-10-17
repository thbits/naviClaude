package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The overlay keeps every section, and on a short terminal switches to two
// columns rather than overflowing the screen.
func TestHelpFitsAndListsAllSections(t *testing.T) {
	for _, size := range []struct{ w, h int }{{200, 60}, {110, 40}, {100, 30}} {
		h := NewHelp()
		h.SetSize(size.w, size.h)
		h.Show()
		v := h.View()
		if got := lipgloss.Height(v); got > size.h {
			t.Errorf("%dx%d: help is %d rows tall", size.w, size.h, got)
		}
		if got := lipgloss.Width(v); got > size.w {
			t.Errorf("%dx%d: help is %d cols wide", size.w, size.h, got)
		}
		for _, want := range []string{"List Mode", "Passthrough Mode", "Changed Files", "Theme picker",
			"Rename session", "New tmux session", "Detail", "Stats", "Scroll preview", "Open in $EDITOR"} {
			if !strings.Contains(v, want) {
				t.Errorf("%dx%d: help missing %q", size.w, size.h, want)
			}
		}
	}
}
