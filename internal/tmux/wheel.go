package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// WheelForwardable reports whether target's app tracks the mouse with SGR, so a
// forwarded wheel scrolls its internal scrollback, which capture-pane can't see.
func (c *Client) WheelForwardable(target string) (bool, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target,
		"#{pane_in_mode} #{mouse_any_flag} #{mouse_button_flag} #{mouse_standard_flag} #{mouse_sgr_flag}").Output()
	if err != nil {
		return false, fmt.Errorf("tmux display-message -t %s: %w", target, err)
	}
	return parseWheelForwardable(string(out))
}

// parseWheelForwardable parses WheelForwardable's five 0/1 flags. A pane in
// copy-mode is excluded: send-keys would feed the bytes to copy-mode instead.
func parseWheelForwardable(raw string) (bool, error) {
	fields := strings.Fields(raw)
	if len(fields) != 5 {
		return false, fmt.Errorf("expected 5 mouse fields, got %q", raw)
	}
	var f [5]bool
	for i, s := range fields {
		n, err := strconv.Atoi(s)
		if err != nil {
			return false, fmt.Errorf("parse mouse field %q: %w", s, err)
		}
		f[i] = n != 0
	}
	inMode, tracking, sgr := f[0], f[1] || f[2] || f[3], f[4]
	return !inMode && tracking && sgr, nil
}

// SendWheel delivers n wheel events at the pane's 1-based cell (x, y).
func (c *Client) SendWheel(target string, up bool, x, y, n int) error {
	if err := exec.Command("tmux", "send-keys", "-t", target, "-l", wheelSequence(up, x, y, n)).Run(); err != nil {
		return fmt.Errorf("tmux send-keys -t %s: %w", target, err)
	}
	return nil
}

// wheelSequence encodes n SGR mouse wheel presses (button 64 up, 65 down).
func wheelSequence(up bool, x, y, n int) string {
	btn := 65
	if up {
		btn = 64
	}
	return strings.Repeat(fmt.Sprintf("\x1b[<%d;%d;%dM", btn, x, y), n)
}
