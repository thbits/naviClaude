package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// wheelFormat reads the flags parseWheelForwardable decides on.
const wheelFormat = "#{pane_in_mode} #{mouse_any_flag} #{mouse_button_flag} #{mouse_standard_flag} #{mouse_sgr_flag}"

// PaneSnapshot is a pane capture plus the mouse mode and cursor read with it.
type PaneSnapshot struct {
	Content string
	// WheelForwardable is true when the pane's app tracks the mouse with SGR, so a
	// forwarded wheel scrolls its internal scrollback, which capture-pane can't see.
	WheelForwardable bool
	Cursor           CursorInfo
}

// CaptureSnapshot captures target's scrollback with ANSI colors, its mouse mode
// and its cursor in one tmux process; the preview runs it every refresh tick.
func (c *Client) CaptureSnapshot(target string) (PaneSnapshot, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target,
		wheelFormat+" #{cursor_x} #{cursor_y} #{pane_height} #{cursor_flag}",
		";", "capture-pane", "-e", "-p", "-S", "-", "-t", target).Output()
	if err != nil {
		return PaneSnapshot{}, fmt.Errorf("tmux capture-pane -t %s: %w", target, err)
	}
	return parseSnapshot(string(out))
}

// parseSnapshot splits CaptureSnapshot's flags line from the capture below it.
func parseSnapshot(raw string) (PaneSnapshot, error) {
	head, body, _ := strings.Cut(raw, "\n")
	fields := strings.Fields(head)
	if len(fields) != 9 {
		return PaneSnapshot{}, fmt.Errorf("expected 9 snapshot fields, got %q", head)
	}
	fwd, err := parseWheelForwardable(strings.Join(fields[:5], " "))
	if err != nil {
		return PaneSnapshot{}, err
	}
	cur, err := parseCursorInfo(strings.Join(fields[5:], " "))
	if err != nil {
		return PaneSnapshot{}, err
	}
	return PaneSnapshot{Content: body, WheelForwardable: fwd, Cursor: cur}, nil
}

// parseWheelForwardable parses wheelFormat's five 0/1 flags. A pane in
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
