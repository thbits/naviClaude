package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// optFloat marks a floating pane opened by the launcher, so a second launch in
// the same window can tell its own modal pane from someone else's.
const optFloat = "@naviclaude_float"

// LaunchTarget is the tmux context a launcher runs in. run-shell jobs have no
// client or current pane, so the binding passes them explicitly.
type LaunchTarget struct {
	Client string // -c target client, e.g. "#{client_name}"
	Pane   string // -t target pane, e.g. "#{pane_id}"
}

// ServerInfo is what the launcher needs to know about the target window.
type ServerInfo struct {
	Version      string // server version, e.g. "3.8", "3.7c", "next-3.9"
	WindowWidth  int
	WindowHeight int
	ModalPane    string // pane id of the window's modal pane; always "" before 3.8
}

// QueryServer asks the server (not the tmux binary on PATH) for its version
// and the target window's geometry.
func (c *Client) QueryServer(t LaunchTarget) (ServerInfo, error) {
	format := strings.Join([]string{"#{version}", "#{window_width}", "#{window_height}", "#{window_modal_pane}"}, paneFieldSep)
	args := append([]string{"display-message", "-p"}, t.args()...)
	out, err := exec.Command("tmux", append(args, format)...).Output()
	if err != nil {
		return ServerInfo{}, fmt.Errorf("tmux display-message: %w", err)
	}
	return parseServerInfo(string(out))
}

func parseServerInfo(out string) (ServerInfo, error) {
	f := strings.Split(strings.TrimRight(out, "\n"), paneFieldSep)
	if len(f) != 4 {
		return ServerInfo{}, fmt.Errorf("unexpected tmux display-message output: %q", out)
	}
	w, errW := strconv.Atoi(f[1])
	h, errH := strconv.Atoi(f[2])
	if errW != nil || errH != nil {
		return ServerInfo{}, fmt.Errorf("unexpected window size in %q", out)
	}
	return ServerInfo{Version: f[0], WindowWidth: w, WindowHeight: h, ModalPane: f[3]}, nil
}

// SupportsFloatingPanes reports whether a server has modal floating panes (new-pane -O,
// tmux 3.8); unparseable or vendor versions fall back to display-popup.
func SupportsFloatingPanes(version string) bool {
	return versionAtLeast(version, 3, 8)
}

// SupportsPopupStyle reports whether display-popup takes -s/-S (tmux 3.3+).
func SupportsPopupStyle(version string) bool {
	return versionAtLeast(version, 3, 3)
}

func versionAtLeast(version string, major, minor int) bool {
	maj, min, err := parseVersionString(version)
	if err != nil {
		return false
	}
	return maj > major || (maj == major && min >= minor)
}

// IsLauncherPane reports whether pane was opened by OpenFloatingPane.
func (c *Client) IsLauncherPane(pane string) bool {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", pane, "#{"+optFloat+"}").Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

// FloatingPaneOptions configures a modal floating pane.
type FloatingPaneOptions struct {
	Target        LaunchTarget
	WidthPercent  int // of the window, like popup_width
	HeightPercent int
	WindowWidth   int // geometry of the target window, used to centre the pane
	WindowHeight  int
	Title         string
	Command       string // a shell command line
}

// floatingPaneArgs builds a modal (-O) new-pane. -C and -D close by killing the pane, skipping
// naviClaude's window-size restore (and Esc is a naviClaude key); -K would swallow the prefix.
func floatingPaneArgs(o FloatingPaneOptions) []string {
	w := percentOf(o.WindowWidth, o.WidthPercent)
	h := percentOf(o.WindowHeight, o.HeightPercent)
	// -x/-y include the border and -X/-Y place its top-left corner.
	args := []string{"new-pane", "-O", "-P", "-F", "#{pane_id}"}
	if o.Target.Pane != "" {
		args = append(args, "-t", o.Target.Pane)
	}
	args = append(args,
		"-x", strconv.Itoa(w), "-y", strconv.Itoa(h),
		"-X", strconv.Itoa((o.WindowWidth-w)/2), "-Y", strconv.Itoa((o.WindowHeight-h)/2))
	if o.Title != "" {
		args = append(args, "-T", o.Title)
	}
	// No -t: new-pane makes the new pane the sequence's current target, so a global
	// remain-on-exit is cleared before the command can exit and leave a dead modal pane.
	return append(args, o.Command,
		";", "set-option", "-pq", "remain-on-exit", "off",
		";", "set-option", "-pq", optFloat, "1")
}

// OpenFloatingPane opens a modal floating pane (tmux 3.8+) and returns its id.
func (c *Client) OpenFloatingPane(o FloatingPaneOptions) (string, error) {
	out, err := exec.Command("tmux", floatingPaneArgs(o)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux new-pane: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func percentOf(total, pct int) int {
	if pct <= 0 || pct > 100 {
		pct = 100
	}
	return total * pct / 100
}

// ShellQuote quotes s as one POSIX shell word; tmux runs a single
// shell-command argument through its default-shell with -c.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (t LaunchTarget) args() []string {
	var args []string
	if t.Client != "" {
		args = append(args, "-c", t.Client)
	}
	if t.Pane != "" {
		args = append(args, "-t", t.Pane)
	}
	return args
}

// Notify shows msg in the target client's status line; run-shell discards a
// failing job's stderr, so this is the only place a launcher error is seen.
func (c *Client) Notify(t LaunchTarget, msg string) {
	exec.Command("tmux", notifyArgs(t, msg)...).Run()
}

// notifyArgs escapes msg's '#', since display-message expands formats and
// "#(cmd)" in an error or exe path would run a shell command.
func notifyArgs(t LaunchTarget, msg string) []string {
	args := append([]string{"display-message"}, t.args()...)
	return append(args, strings.ReplaceAll(msg, "#", "##"))
}
