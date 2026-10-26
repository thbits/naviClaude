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

func TestParseSnapshot(t *testing.T) {
	snap, err := parseSnapshot("0 1 0 0 1 3 20 24 1\n\x1b[31mred\x1b[0m\nsecond\n")
	if err != nil {
		t.Fatalf("parseSnapshot: %v", err)
	}
	if !snap.WheelForwardable {
		t.Error("WheelForwardable = false, want true")
	}
	if want := (CursorInfo{X: 3, Y: 20, PaneHeight: 24, Visible: true}); snap.Cursor != want {
		t.Errorf("Cursor = %+v, want %+v", snap.Cursor, want)
	}
	if want := "\x1b[31mred\x1b[0m\nsecond\n"; snap.Content != want {
		t.Errorf("Content = %q, want %q", snap.Content, want)
	}
	// An empty pane still yields its flags line.
	if snap, err := parseSnapshot("0 0 0 0 0 0 0 24 0\n"); err != nil || snap.Content != "" {
		t.Errorf("empty pane = %+v, %v", snap, err)
	}
	for _, bad := range []string{"", "0 1 0 0 1\nbody", "0 1 0 0 1 3 x 24 1\n"} {
		if _, err := parseSnapshot(bad); err == nil {
			t.Errorf("parseSnapshot(%q) succeeded, want error", bad)
		}
	}
}
