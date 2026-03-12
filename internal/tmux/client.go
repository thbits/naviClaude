package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Client wraps tmux CLI commands via os/exec.
type Client struct{}

// New returns a new Client.
func New() *Client {
	return &Client{}
}

// IsRunning reports whether a tmux server is currently running.
func (c *Client) IsRunning() bool {
	err := exec.Command("tmux", "info").Run()
	return err == nil
}

// Version returns the tmux version as a string, e.g. "3.4".
// Returns an error if tmux is not found or the output cannot be parsed.
func (c *Client) Version() (string, error) {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return "", fmt.Errorf("tmux not found: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	// output is "tmux 3.4" or similar
	parts := strings.Fields(raw)
	if len(parts) < 2 {
		return "", fmt.Errorf("unexpected tmux -V output: %q", raw)
	}
	return parts[1], nil
}

// CheckVersion verifies that the installed tmux version is >= the given major.minor.
func (c *Client) CheckVersion(major, minor int) error {
	v, err := c.Version()
	if err != nil {
		return err
	}
	maj, min, err := parseVersionString(v)
	if err != nil {
		return fmt.Errorf("cannot parse tmux version %q: %w", v, err)
	}
	if maj > major || (maj == major && min >= minor) {
		return nil
	}
	return fmt.Errorf("tmux >= %d.%d required, found %d.%d", major, minor, maj, min)
}

// ListPanes returns all panes across all sessions.
func (c *Client) ListPanes() ([]PaneInfo, error) {
	format := "#{session_name}:#{window_index}.#{pane_index} #{pane_current_command} #{pane_pid} #{pane_current_path}"
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", format).Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}
	return ParsePanes(string(out))
}

// CapturePaneOutput captures the visible contents of a pane, preserving ANSI
// color sequences.  target is a tmux target string such as "session:1.0".
func (c *Client) CapturePaneOutput(target string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-e", "-p", "-t", target).Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane -t %s: %w", target, err)
	}
	return string(out), nil
}

// KillPane kills the specified tmux pane (and its process).
func (c *Client) KillPane(target string) error {
	if err := exec.Command("tmux", "kill-pane", "-t", target).Run(); err != nil {
		return fmt.Errorf("tmux kill-pane -t %s: %w", target, err)
	}
	return nil
}

// SendKeys sends key strokes to a pane.  keys may be a literal string or a
// named key such as "Enter".
func (c *Client) SendKeys(target, keys string) error {
	if err := exec.Command("tmux", "send-keys", "-t", target, keys).Run(); err != nil {
		return fmt.Errorf("tmux send-keys -t %s: %w", target, err)
	}
	return nil
}

// SwitchClient switches the tmux client to the given target window/pane.
func (c *Client) SwitchClient(target string) error {
	if err := exec.Command("tmux", "switch-client", "-t", target).Run(); err != nil {
		return fmt.Errorf("tmux switch-client -t %s: %w", target, err)
	}
	return nil
}

// SelectPane makes the specified pane the active pane in its window.
func (c *Client) SelectPane(target string) error {
	if err := exec.Command("tmux", "select-pane", "-t", target).Run(); err != nil {
		return fmt.Errorf("tmux select-pane -t %s: %w", target, err)
	}
	return nil
}

// DisplayPopupOptions holds parameters for tmux display-popup.
type DisplayPopupOptions struct {
	// Width and Height accept tmux size expressions, e.g. "80" or "80%".
	Width   string
	Height  string
	Command string
}

// DisplayPopup opens an interactive popup over the current pane.
// The popup runs Command and exits when the command finishes.
func (c *Client) DisplayPopup(opts DisplayPopupOptions) error {
	args := []string{"display-popup", "-E"}
	if opts.Width != "" {
		args = append(args, "-w", opts.Width)
	}
	if opts.Height != "" {
		args = append(args, "-h", opts.Height)
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}
	if err := exec.Command("tmux", args...).Run(); err != nil {
		return fmt.Errorf("tmux display-popup: %w", err)
	}
	return nil
}

// SplitWindowOptions holds parameters for tmux split-window.
type SplitWindowOptions struct {
	Target  string // session or window target
	Command string // optional command to run in the new pane
}

// SplitWindow splits the target window and optionally runs a command in the
// new pane.
func (c *Client) SplitWindow(opts SplitWindowOptions) error {
	args := []string{"split-window"}
	if opts.Target != "" {
		args = append(args, "-t", opts.Target)
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}
	if err := exec.Command("tmux", args...).Run(); err != nil {
		return fmt.Errorf("tmux split-window: %w", err)
	}
	return nil
}

// NewWindowOptions holds parameters for tmux new-window.
type NewWindowOptions struct {
	Target  string // session target, e.g. "mysession:"
	Name    string // optional window name (-n flag)
	Command string // optional command to run in the new window
}

// NewWindow creates a new window in the target session and optionally runs a
// command in it.
func (c *Client) NewWindow(opts NewWindowOptions) error {
	args := []string{"new-window"}
	if opts.Target != "" {
		args = append(args, "-t", opts.Target)
	}
	if opts.Name != "" {
		args = append(args, "-n", opts.Name)
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}
	if err := exec.Command("tmux", args...).Run(); err != nil {
		return fmt.Errorf("tmux new-window: %w", err)
	}
	return nil
}

// NewWindowPrint creates a new window and returns its target identifier
// (e.g. "session:3.0"). Uses -P -F to print the pane info.
func (c *Client) NewWindowPrint(opts NewWindowOptions) (string, error) {
	format := "#{session_name}:#{window_index}.#{pane_index}"
	args := []string{"new-window", "-d", "-P", "-F", format}
	if opts.Target != "" {
		args = append(args, "-t", opts.Target)
	}
	if opts.Name != "" {
		args = append(args, "-n", opts.Name)
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux new-window: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// parseVersionString parses a version string like "3.4" or "3.4a" into its
// major and minor integer components.
func parseVersionString(v string) (major, minor int, err error) {
	// strip trailing non-numeric suffix (e.g. "3.4a" -> "3.4")
	trimmed := strings.TrimRightFunc(v, func(r rune) bool {
		return r < '0' || r > '9'
	})
	// restore last digit group if we over-trimmed
	if trimmed == "" {
		trimmed = v
	}
	parts := strings.SplitN(trimmed, ".", 2)
	if len(parts) == 0 {
		return 0, 0, fmt.Errorf("empty version")
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	if len(parts) == 2 && parts[1] != "" {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, err
		}
	}
	return major, minor, nil
}
