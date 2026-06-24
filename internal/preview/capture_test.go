package preview

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Claude Code's trust/permission prompts render at the top of the pane and do
// not fill the screen, so `tmux capture-pane -S -` returns the prompt followed
// by many blank lines (the rest of the pane grid). The preview viewport
// auto-scrolls to the bottom to follow live output, so unless those trailing
// blank lines are removed the prompt scrolls off the top and the preview looks
// empty. trimTrailingBlankLines must drop trailing blank lines so the last line
// is meaningful content.
func TestTrimTrailingBlankLines(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "drops trailing empty lines (top-anchored trust prompt)",
			in:   []string{"❯ 1. Yes, I trust this folder", "  2. No, exit", "", "", "", ""},
			want: []string{"❯ 1. Yes, I trust this folder", "  2. No, exit"},
		},
		{
			name: "drops trailing whitespace-only lines",
			in:   []string{"content", "   ", "\t", " "},
			want: []string{"content"},
		},
		{
			name: "drops trailing lines that are only ANSI escape codes",
			in:   []string{"content", "\x1b[39m", "\x1b[0m\x1b[39m"},
			want: []string{"content"},
		},
		{
			name: "preserves blank lines between content (only trailing trimmed)",
			in:   []string{"top", "", "middle", "", "bottom", "", ""},
			want: []string{"top", "", "middle", "", "bottom"},
		},
		{
			name: "all-blank input collapses to empty so the caller shows a waiting placeholder",
			in:   []string{"", "   ", "\x1b[39m"},
			want: []string{},
		},
		{
			name: "no trailing blanks is unchanged (bottom-anchored active session)",
			in:   []string{"line a", "line b"},
			want: []string{"line a", "line b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimTrailingBlankLines(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("trimTrailingBlankLines(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// renderCursorOnLine must draw a reverse-video block at the cursor cell while
// leaving the visible text (ANSI stripped) and its display width unchanged, so
// the preview shows where input lands without shifting any content.
func TestRenderCursorOnLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		x       int
		wantTxt string // expected ANSI-stripped text
	}{
		{
			name:    "cursor mid-text preserves surrounding characters",
			line:    "hello",
			x:       2,
			wantTxt: "hello",
		},
		{
			name:    "cursor at start of line",
			line:    "abc",
			x:       0,
			wantTxt: "abc",
		},
		{
			name:    "cursor past end of text pads with spaces",
			line:    "ab",
			x:       5,
			wantTxt: "ab    ", // 2 chars + 3 pad + 1 block space
		},
		{
			name:    "cursor exactly at end of text draws a trailing block",
			line:    "ab",
			x:       2,
			wantTxt: "ab ",
		},
		{
			name:    "cursor inside an ANSI-colored input box",
			line:    "\x1b[34m│\x1b[0m > type here \x1b[34m│\x1b[0m",
			x:       4,
			wantTxt: "│ > type here │",
		},
		{
			name:    "empty line draws a block at the cursor column",
			line:    "",
			x:       0,
			wantTxt: " ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderCursorOnLine(tt.line, tt.x)

			if stripped := ansi.Strip(got); stripped != tt.wantTxt {
				t.Errorf("text = %q, want %q (raw %q)", stripped, tt.wantTxt, got)
			}
			if !strings.Contains(got, cursorReverseOn) {
				t.Errorf("expected reverse-video block, got %q", got)
			}
			// The cursor cell is zero-width chrome, so the rendered width must
			// equal the original width or the padded cursor column + 1.
			gotWidth := ansi.StringWidth(got)
			origWidth := ansi.StringWidth(tt.line)
			wantWidth := origWidth
			if tt.x >= origWidth {
				wantWidth = tt.x + 1
			}
			if gotWidth != wantWidth {
				t.Errorf("width = %d, want %d", gotWidth, wantWidth)
			}
		})
	}
}

// drawCursor maps a (row, x) onto the line slice and must be a safe no-op when
// the target falls outside the rendered content or the viewport width.
func TestDrawCursor(t *testing.T) {
	base := []string{"line0", "line1", "line2"}

	t.Run("draws on the target row only", func(t *testing.T) {
		lines := append([]string(nil), base...)
		out := drawCursor(lines, 1, 2, 0)
		if !strings.Contains(out[1], cursorReverseOn) {
			t.Errorf("row 1 should have a cursor, got %q", out[1])
		}
		if strings.Contains(out[0], cursorReverseOn) || strings.Contains(out[2], cursorReverseOn) {
			t.Errorf("only row 1 should change: %q", out)
		}
	})

	noopCases := []struct {
		name             string
		row, x, maxWidth int
	}{
		{"row below range", -1, 0, 0},
		{"row above range", 3, 0, 0},
		{"negative column", 0, -1, 0},
		{"column past maxWidth", 0, 80, 40},
	}
	for _, tc := range noopCases {
		t.Run("no-op: "+tc.name, func(t *testing.T) {
			lines := append([]string(nil), base...)
			out := drawCursor(lines, tc.row, tc.x, tc.maxWidth)
			if !reflect.DeepEqual(out, base) {
				t.Errorf("expected unchanged %q, got %q", base, out)
			}
		})
	}
}
