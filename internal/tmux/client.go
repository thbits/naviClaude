package tmux

import (
	"fmt"
	"os"
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
	// FieldSep keeps spaces in names and paths intact; keep in sync with parsePaneLine.
	format := "#{session_name}:#{window_index}.#{pane_index}" + FieldSep +
		"#{pane_current_command}" + FieldSep +
		"#{pane_pid}" + FieldSep +
		"#{pane_id}" + FieldSep +
		"#{@naviclaude_attn}" + FieldSep +
		"#{pane_current_path}"
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", format).Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}
	return ParsePanes(string(out))
}

// CapturePaneOutput captures a pane's full scrollback and visible rows with ANSI
// colors kept.
func (c *Client) CapturePaneOutput(target string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-e", "-p", "-S", "-", "-t", target).Output()
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

// PaneExists reports whether target names a live pane.
func (c *Client) PaneExists(target string) bool {
	return exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_id}").Run() == nil
}

// PaneID returns target's stable pane id (e.g. "%12"), or "" if it can't be read.
func (c *Client) PaneID(target string) string {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_id}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SendKeys sends key strokes to a pane.  keys may be a literal string or a
// named key such as "Enter".
func (c *Client) SendKeys(target, keys string) error {
	if err := exec.Command("tmux", "send-keys", "-t", target, keys).Run(); err != nil {
		return fmt.Errorf("tmux send-keys -t %s: %w", target, err)
	}
	return nil
}

// PasteText pastes text into a pane as tmux's own paste does, bracketed when the
// app asked for it, so a multi-line paste isn't submitted line by line.
func (c *Client) PasteText(target, text string) error {
	const buf = "naviclaude-paste"
	cmd := exec.Command("tmux", "load-buffer", "-b", buf, "-", ";",
		"paste-buffer", "-p", "-d", "-b", buf, "-t", target)
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux paste-buffer -t %s: %w: %s", target, err, strings.TrimSpace(string(out)))
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

// SelectPane makes target the active pane. tmux ignores focus behind a modal pane, so
// when that is this process's own floating pane it is closed first, ending the process.
func (c *Client) SelectPane(target string) error {
	if self := os.Getenv("TMUX_PANE"); self != "" && modalPaneOf(target) == self {
		// One invocation: tmux runs both before this process gets its SIGHUP.
		return exec.Command("tmux", "kill-pane", "-t", self, ";", "select-pane", "-t", target).Run()
	}
	if err := exec.Command("tmux", "select-pane", "-t", target).Run(); err != nil {
		return fmt.Errorf("tmux select-pane -t %s: %w", target, err)
	}
	return nil
}

// modalPaneOf returns the id of the modal pane in target's window, or "" when
// there is none (always, before tmux 3.8).
func modalPaneOf(target string) string {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{window_modal_pane}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// DisplayPopupOptions holds parameters for tmux display-popup.
type DisplayPopupOptions struct {
	Target LaunchTarget
	// Width and Height accept tmux size expressions, e.g. "80" or "80%".
	Width   string
	Height  string
	Command string
	Version string // server version; gates the -s/-S style flags
}

func displayPopupArgs(opts DisplayPopupOptions) []string {
	args := append([]string{"display-popup", "-E"}, opts.Target.args()...)
	if opts.Width != "" {
		args = append(args, "-w", opts.Width)
	}
	if opts.Height != "" {
		args = append(args, "-h", opts.Height)
	}
	args = append(args, "-x", "C", "-y", "C")
	if SupportsPopupStyle(opts.Version) {
		args = append(args, "-s", PopupStyle, "-S", PopupStyle)
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}
	return args
}

// DisplayPopup opens a centred interactive popup that closes when Command
// exits. It blocks until then.
func (c *Client) DisplayPopup(opts DisplayPopupOptions) error {
	if out, err := exec.Command("tmux", displayPopupArgs(opts)...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux display-popup: %w: %s", err, strings.TrimSpace(string(out)))
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

// NewSessionOptions holds parameters for tmux new-session.
type NewSessionOptions struct {
	Name       string // session name (-s flag)
	WindowName string // initial window name (-n flag)
	StartDir   string // working directory (-c flag)
}

// NewSessionPrint creates a new detached tmux session and returns the target
// identifier of its first pane (e.g. "newsess:0.0").
func (c *Client) NewSessionPrint(opts NewSessionOptions) (string, error) {
	args := []string{"new-session", "-d", "-P", "-F", printFormat}
	if opts.Name != "" {
		args = append(args, "-s", opts.Name)
	}
	if opts.WindowName != "" {
		args = append(args, "-n", opts.WindowName)
	}
	if opts.StartDir != "" {
		args = append(args, "-c", opts.StartDir)
	}
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux new-session: %w", err)
	}
	return createdTarget(out, opts.WindowName), nil
}

// printFormat makes new-session/new-window print the window id and pane target.
const printFormat = "#{window_id}" + FieldSep + "#{session_name}:#{window_index}.#{pane_index}"

// createdTarget parses printFormat output into the pane target. A named window gets
// allow-rename off (and its name re-applied) so shell title escapes can't retitle it.
func createdTarget(out []byte, name string) string {
	id, target, _ := strings.Cut(strings.TrimSpace(string(out)), FieldSep)
	if name != "" && id != "" {
		exec.Command("tmux", "set-option", "-w", "-t", id, "allow-rename", "off", ";",
			"rename-window", "-t", id, name).Run()
	}
	return target
}

// ServerIdentity returns the running server's start time and socket path.
func (c *Client) ServerIdentity() (ServerIdentity, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "#{start_time}"+FieldSep+"#{socket_path}").Output()
	if err != nil {
		return ServerIdentity{}, fmt.Errorf("tmux display-message: %w", err)
	}
	return ParseServerIdentity(string(out))
}

// PaneCommand returns the command running in the pane at target.
func (c *Client) PaneCommand(target string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_current_command}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux display-message: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// HasSession reports whether a session named exactly name exists; the "=" prefix
// stops "dev" matching "dev-tools".
func (c *Client) HasSession(name string) bool {
	if name == "" {
		return false
	}
	return exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil
}

// ResizeToClient resizes all windows in a session to match the current client
// dimensions. This is needed for detached sessions which default to 80x24.
func (c *Client) ResizeToClient(sessionName string) error {
	// Get client dimensions.
	out, err := exec.Command("tmux", "display-message", "-p", "#{client_width} #{client_height}").Output()
	if err != nil {
		return err
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return fmt.Errorf("unexpected tmux display-message output: %q", string(out))
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return err
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return err
	}
	return c.ResizeSession(sessionName, w, h)
}

// ResizeSession resizes all windows in a session to the given dimensions.
func (c *Client) ResizeSession(sessionName string, w, h int) error {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	// List all windows in the session so we can resize each one.
	out, err := exec.Command("tmux", "list-windows", "-t", sessionName, "-F", "#{window_index}").Output()
	if err != nil {
		// Fallback: try resizing just the session target.
		exec.Command("tmux", "resize-window", "-t", sessionName+":", "-x", strconv.Itoa(w), "-y", strconv.Itoa(h)).Run()
		return nil
	}

	ws := strconv.Itoa(w)
	hs := strconv.Itoa(h)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		target := sessionName + ":" + line
		exec.Command("tmux", "resize-window", "-t", target, "-x", ws, "-y", hs).Run()
	}
	return nil
}

// WindowSize returns the current width and height of a tmux window.
func (c *Client) WindowSize(target string) (int, int, error) {
	out, err := exec.Command("tmux", "display-message", "-t", target, "-p", "#{window_width} #{window_height}").Output()
	if err != nil {
		return 0, 0, err
	}
	return parseWindowSize(string(out))
}

// parseWindowSize parses "#{window_width} #{window_height}" output.
func parseWindowSize(out string) (int, int, error) {
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected output: %q", out)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

// ResizeWindow resizes a single tmux window. Pass 0 for w or h to leave
// that dimension unchanged.
func (c *Client) ResizeWindow(target string, w, h int) error {
	runChain(resizeArgs(target, w, h))
	return nil
}

func resizeArgs(target string, w, h int) []string {
	args := []string{"resize-window", "-t", target}
	if w > 0 {
		args = append(args, "-x", strconv.Itoa(w))
	}
	if h > 0 {
		args = append(args, "-y", strconv.Itoa(h))
	}
	if len(args) <= 3 {
		return nil // nothing to resize
	}
	return args
}

// optPreSize records a window's own window-size option before naviClaude first
// resized it ("auto" when it was inherited), so the restore survives a crash.
const optPreSize = "@naviclaude_presize"

// readMarker separates the new window's options from the old one's in a read chain;
// it is printable for the same reason as FieldSep.
const readMarker = "naviclaude-prev-window="

// chain joins tmux commands with ";" so they run in one tmux process.
func chain(cmds ...[]string) []string {
	var args []string
	for _, cmd := range cmds {
		if len(cmd) == 0 {
			continue
		}
		if len(args) > 0 {
			args = append(args, ";")
		}
		args = append(args, cmd...)
	}
	return args
}

func runChain(args []string) {
	if len(args) > 0 {
		exec.Command("tmux", args...).Run()
	}
}

// showOptionsArgs reads the window's own (not inherited) window-size and presize;
// show-options -q prints nothing for unset options and missing windows alike.
func showOptionsArgs(target string) []string {
	return chain(
		[]string{"show-options", "-wq", "-t", target, "window-size"},
		[]string{"show-options", "-wq", "-t", target, optPreSize},
	)
}

// windowOptions is a window's own window-size and presize; "" means unset.
type windowOptions struct {
	size, preSize string
}

func parseWindowOptions(lines []string) windowOptions {
	var o windowOptions
	for _, line := range lines {
		name, value := parseOptionLine(line)
		switch name {
		case "window-size":
			o.size = value
		case optPreSize:
			o.preSize = value
		}
	}
	return o
}

// parseOptionLine splits a show-options line into its name and value, dropping
// the quotes tmux puts around empty or spaced values.
func parseOptionLine(line string) (string, string) {
	name, value, _ := strings.Cut(strings.TrimSpace(line), " ")
	value = strings.TrimSpace(value)
	if n := len(value); n >= 2 && (value[0] == '\'' || value[0] == '"') && value[n-1] == value[0] {
		value = value[1 : n-1]
	}
	return name, value
}

// saveArgs records the window's pre-resize window-size unless an earlier run
// never restored, in which case the recorded value is the true original.
func saveArgs(target string, o windowOptions) []string {
	if o.preSize != "" {
		return nil
	}
	pre := o.size
	if pre == "" {
		pre = "auto"
	}
	return []string{"set-option", "-wq", "-t", target, optPreSize, pre}
}

// restoreArgs undoes a preview resize given the recorded presize. resize-window
// pins window-size to manual, so it must run before window-size is reset.
func restoreArgs(target string, w, h int, pre string) []string {
	unsetPre := []string{"set-option", "-wqu", "-t", target, optPreSize}
	switch pre {
	case "":
		return resizeArgs(target, w, h)
	case "auto":
		return chain(
			[]string{"resize-window", "-A", "-t", target},
			[]string{"set-option", "-wqu", "-t", target, "window-size"},
			unsetPre,
		)
	default:
		return chain(
			resizeArgs(target, w, h),
			[]string{"set-option", "-wq", "-t", target, "window-size", pre},
			unsetPre,
		)
	}
}

// SaveWindowSize remembers how the window was sized before a preview resize.
// resize-window pins window-size to manual, which RestoreWindowSize undoes.
func (c *Client) SaveWindowSize(target string) {
	out, _ := exec.Command("tmux", showOptionsArgs(target)...).Output()
	runChain(saveArgs(target, parseWindowOptions(strings.Split(string(out), "\n"))))
}

// RestoreWindowSize undoes a preview resize: a window that followed its clients
// follows them again; one with its own window-size gets w x h and that setting.
func (c *Client) RestoreWindowSize(target string, w, h int) {
	out, _ := exec.Command("tmux", "show-options", "-wq", "-t", target, optPreSize).Output()
	o := parseWindowOptions(strings.Split(string(out), "\n"))
	runChain(restoreArgs(target, w, h, o.preSize))
}

// previewReadArgs reads target's size and options and, when prev is set, prev's
// presize after a marker line, all in one tmux process.
func previewReadArgs(target, prev string) []string {
	args := chain(
		[]string{"display-message", "-p", "-t", target, "#{window_width} #{window_height} #{window_id}"},
		showOptionsArgs(target),
	)
	if prev != "" {
		args = chain(args,
			[]string{"display-message", "-p", "-t", prev, readMarker + "#{window_id}"},
			[]string{"show-options", "-wq", "-t", prev, optPreSize},
		)
	}
	return args
}

// previewRead is the parsed output of previewReadArgs.
type previewRead struct {
	w, h    int
	sizeErr error
	opts    windowOptions
	prevPre string
	// winID and prevWinID tell grouped or linked windows apart from distinct ones.
	winID, prevWinID string
}

func parsePreviewRead(out string) previewRead {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var r previewRead
	size := strings.Fields(lines[0])
	if len(size) == 3 {
		r.winID, size = size[2], size[:2]
	}
	r.w, r.h, r.sizeErr = parseWindowSize(strings.Join(size, " "))
	cur, prev := lines[1:], []string(nil)
	for i, line := range cur {
		if id, ok := strings.CutPrefix(line, readMarker); ok {
			cur, prev, r.prevWinID = cur[:i], cur[i+1:], id
			break
		}
	}
	r.opts = parseWindowOptions(cur)
	r.prevPre = parseWindowOptions(prev).preSize
	return r
}

// FitPreviewWindow restores prev (if set) to prevW x prevH and fits target to
// w x h, returning target's size before the fit; both windows share one read.
func (c *Client) FitPreviewWindow(target string, w, h int, prev string, prevW, prevH int) (int, int, error) {
	if prev == target {
		// The read must see the restored window, so this rare case stays sequential.
		c.RestoreWindowSize(prev, prevW, prevH)
		prev = ""
	}
	out, _ := exec.Command("tmux", previewReadArgs(target, prev)...).Output()
	r := parsePreviewRead(string(out))
	if prev != "" && r.winID != "" && r.winID == r.prevWinID {
		// Grouped sessions name one window twice; restore it before reading it.
		c.RestoreWindowSize(prev, prevW, prevH)
		out, _ = exec.Command("tmux", previewReadArgs(target, "")...).Output()
		r, prev = parsePreviewRead(string(out)), ""
	}
	// Separate processes: a vanished window's resize error would abort the rest of a chain.
	if prev != "" {
		runChain(restoreArgs(prev, prevW, prevH, r.prevPre))
	}
	runChain(chain(saveArgs(target, r.opts), resizeArgs(target, w, h)))
	return r.w, r.h, r.sizeErr
}

// WindowTarget extracts the "session:window" portion from a pane target
// like "session:1.0", stripping the ".pane" suffix.
func WindowTarget(paneTarget string) string {
	if dot := strings.LastIndex(paneTarget, "."); dot != -1 {
		return paneTarget[:dot]
	}
	return paneTarget
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
	args := []string{"new-window", "-P", "-F", printFormat}
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
		return fmt.Errorf("tmux new-window: %w", err)
	}
	createdTarget(out, opts.Name)
	return nil
}

// NewWindowPrint creates a new window and returns its target identifier
// (e.g. "session:3.0"). Uses -P -F to print the pane info.
func (c *Client) NewWindowPrint(opts NewWindowOptions) (string, error) {
	args := []string{"new-window", "-d", "-P", "-F", printFormat}
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
	return createdTarget(out, opts.Name), nil
}

// parseVersionString parses a version string like "3.4", "3.4a" or
// "next-3.9" (a development build) into its major and minor components.
func parseVersionString(v string) (major, minor int, err error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "next-")
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
	if len(parts) == 2 {
		// Keep only the leading digits, so "8-rc2" reads as 8.
		digits := parts[1]
		if i := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
			digits = digits[:i]
		}
		if digits != "" {
			if minor, err = strconv.Atoi(digits); err != nil {
				return 0, 0, err
			}
		}
	}
	return major, minor, nil
}
