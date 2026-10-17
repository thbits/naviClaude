package tmux

import "testing"

func TestParseWheelForwardable(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"0 1 0 0 1\n", true}, // any-event tracking, SGR (Claude Code)
		{"0 0 1 0 1", true},   // button-event tracking
		{"0 0 0 1 1", true},   // normal tracking
		{"0 0 0 0 0", false},  // plain shell
		{"0 1 0 0 0", false},  // legacy X10 encoding only
		{"1 1 0 0 1", false},  // copy-mode would eat the bytes
		{"0 0 0 0 1", false},  // SGR flag without tracking
	}
	for _, tt := range tests {
		got, err := parseWheelForwardable(tt.raw)
		if err != nil || got != tt.want {
			t.Errorf("parseWheelForwardable(%q) = %v, %v; want %v", tt.raw, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "0 1 0 1", "0 1 x 0 1"} {
		if _, err := parseWheelForwardable(bad); err == nil {
			t.Errorf("parseWheelForwardable(%q) succeeded, want error", bad)
		}
	}
}

func TestWheelSequence(t *testing.T) {
	if got, want := wheelSequence(true, 10, 5, 1), "\x1b[<64;10;5M"; got != want {
		t.Errorf("up = %q, want %q", got, want)
	}
	if got, want := wheelSequence(false, 3, 7, 2), "\x1b[<65;3;7M\x1b[<65;3;7M"; got != want {
		t.Errorf("down x2 = %q, want %q", got, want)
	}
}
