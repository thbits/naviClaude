package preview

import (
	"reflect"
	"testing"
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
