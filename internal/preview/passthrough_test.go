package preview

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTranslateKey(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		// Plain keys are unchanged.
		{"plain enter", tea.KeyMsg{Type: tea.KeyEnter}, "Enter"},
		{"plain rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}, "f"},
		{"plain backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "BSpace"},
		{"plain space", tea.KeyMsg{Type: tea.KeySpace}, "Space"},
		{"plain up", tea.KeyMsg{Type: tea.KeyUp}, "Up"},
		{"ctrl-a", tea.KeyMsg{Type: tea.KeyCtrlA}, "C-a"},

		// Alt-modified keys must carry the Meta (M-) prefix so tmux emits the
		// ESC-prefixed byte sequence the terminal would send directly. This is
		// the fix for Alt+Enter inserting a newline in the Claude prompt instead
		// of submitting.
		{"alt enter", tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, "M-Enter"},
		{"alt rune f", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}, Alt: true}, "M-f"},
		{"alt backspace", tea.KeyMsg{Type: tea.KeyBackspace, Alt: true}, "M-BSpace"},
		{"alt space", tea.KeyMsg{Type: tea.KeySpace, Alt: true}, "M-Space"},
		{"alt up", tea.KeyMsg{Type: tea.KeyUp, Alt: true}, "M-Up"},

		// Unknown keys still produce no output (nothing forwarded).
		{"unhandled", tea.KeyMsg{Type: tea.KeyType(-9999)}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := translateKey(tt.msg); got != tt.want {
				t.Errorf("translateKey(%q) = %q, want %q", tt.msg.String(), got, tt.want)
			}
		})
	}
}
