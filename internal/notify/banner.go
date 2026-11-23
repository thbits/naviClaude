package notify

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Desktop banner modes (notifications.desktop).
const (
	DesktopOff      = "off"
	DesktopSystem   = "system"   // Notification Center via terminal-notifier or osascript
	DesktopTerminal = "terminal" // OSC 9 to the terminal through tmux passthrough
)

// Banner click behaviors (notifications.click).
const (
	ClickNaviclaude = "naviclaude" // open naviClaude with the session selected
	ClickPane       = "pane"       // jump straight to the session's pane
)

// DisableBannerEnv suppresses all banners when set (used by tests and CI).
const DisableBannerEnv = "NAVICLAUDE_DISABLE_BANNER"

// bannerTimeout bounds a detached notifier process so a hung one cannot linger.
const bannerTimeout = 10 * time.Second

// Banner is one desktop notification.
type Banner struct {
	Title    string
	Subtitle string // where the session runs, e.g. "naviClaude ⎇ main"
	Message  string
	Pane     string // tmux pane to focus when the banner is clicked; also groups repeats
}

// Notifier posts banners; every OS-facing call is a field so tests stay silent.
type Notifier struct {
	Mode     string
	Click    string // ClickNaviclaude (default) or ClickPane
	Exe      string // absolute path of naviclaude, run by a clicked banner
	GOOS     string
	LookPath func(file string) (string, error)
	Getenv   func(key string) string
	Exec     func(name string, args ...string) error
	Clients  func() ([]string, error) // ttys of the panes attached tmux clients show
	WriteTTY func(tty string, data []byte) error
	// Passthrough reports whether tmux lets OSC 9 through to the terminal.
	Passthrough func() bool
	// Fallback is why the last Notify could not use terminal-notifier, if it fell back.
	Fallback error
}

// osc9Terminals post OSC 9 as native notifications attributed to themselves.
var osc9Terminals = map[string]bool{
	"com.mitchellh.ghostty":  true,
	"com.googlecode.iterm2":  true,
	"net.kovidgoyal.kitty":   true,
	"com.github.wez.wezterm": true,
}

// NewNotifier returns a Notifier for mode. With wait, commands run to completion
// and report errors; otherwise they are started detached and reaped on a timeout.
func NewNotifier(mode string, wait bool) *Notifier {
	run := startDetached
	if wait {
		run = runWait
	}
	exe, _ := SelfExecutable()
	return &Notifier{
		Mode:        mode,
		Exe:         exe,
		GOOS:        runtime.GOOS,
		LookPath:    exec.LookPath,
		Getenv:      os.Getenv,
		Exec:        run,
		Clients:     clientPaneTTYs,
		WriteTTY:    writeTTY,
		Passthrough: tmuxPassthroughOn,
	}
}

// tmuxPassthroughOn reports whether tmux forwards escape sequences to the terminal.
func tmuxPassthroughOn() bool {
	out, err := exec.Command("tmux", "display-message", "-p", "#{allow-passthrough}").Output()
	v := strings.TrimSpace(string(out))
	return err == nil && (v == "on" || v == "all")
}

// FallsBackToTerminal reports whether a failed terminal-notifier banner goes through the terminal.
func (n *Notifier) FallsBackToTerminal() bool { return n.terminalNotifies() }

// terminalNotifies reports whether the host terminal turns OSC 9 into a native
// banner, which a click brings forward instead of opening Script Editor.
func (n *Notifier) terminalNotifies() bool {
	if !osc9Terminals[n.Getenv("__CFBundleIdentifier")] {
		return false
	}
	return n.Getenv("TMUX") == "" || (n.Passthrough != nil && n.Passthrough())
}

// SetupHint is advice for getting clickable, lasting banners from Backend; "" when there is none.
func (n *Notifier) SetupHint() string {
	switch n.Backend() {
	case "OSC 9":
		if n.Mode == DesktopSystem {
			return "Banners come from your terminal, so a click brings it forward but not the session's pane.\n" +
				"Install terminal-notifier (brew install terminal-notifier) to jump to the pane on click."
		}
	case "osascript":
		return "Banners come from Script Editor, so clicking one won't open the session. Install terminal-notifier\n" +
			"(brew install terminal-notifier) to jump to the session's pane on click."
	case "terminal-notifier":
		return "In System Settings > Notifications > terminal-notifier, allow notifications, and pick Alerts\n" +
			"to keep banners on screen until you dismiss them."
	}
	return ""
}

// osascript posts through AppleScript; macOS attributes it to Script Editor.
func (n *Notifier) osascript(b Banner) error {
	// Passing text as argv avoids AppleScript string escaping entirely.
	return n.Exec("osascript",
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv) subtitle (item 3 of argv)",
		"-e", "end run",
		"--", b.Title, b.Message, b.Subtitle)
}

// Backend names what Notify would use, for messages.
func (n *Notifier) Backend() string {
	switch n.Mode {
	case DesktopSystem:
		if n.GOOS == "darwin" {
			if _, err := n.LookPath("terminal-notifier"); err == nil {
				return "terminal-notifier"
			}
			if n.terminalNotifies() {
				return "OSC 9"
			}
			return "osascript"
		}
		return "notify-send"
	case DesktopTerminal:
		return "OSC 9"
	}
	return DesktopOff
}

// Notify posts b according to the mode; "off" and unknown modes do nothing.
func (n *Notifier) Notify(b Banner) error {
	if n.Getenv(DisableBannerEnv) != "" {
		return nil
	}
	b.Title, b.Subtitle, b.Message = clean(b.Title), clean(b.Subtitle), clean(b.Message)
	switch n.Mode {
	case DesktopSystem:
		return n.system(b)
	case DesktopTerminal:
		return n.terminal(b)
	}
	return nil
}

func (n *Notifier) system(b Banner) error {
	switch n.Backend() {
	case "OSC 9":
		return n.terminal(b)
	case "terminal-notifier":
		tn, _ := n.LookPath("terminal-notifier")
		group := "naviclaude"
		if b.Pane != "" {
			group += "-" + b.Pane
		}
		args := []string{"-title", tnEscape(b.Title), "-message", tnEscape(b.Message), "-group", group}
		if b.Subtitle != "" {
			args = append(args, "-subtitle", tnEscape(b.Subtitle))
		}
		if cmd := n.focusCommand(b.Pane); cmd != "" {
			args = append(args, "-execute", cmd)
		}
		// The terminal app that launched tmux; clicking brings it to the front.
		if id := n.Getenv("__CFBundleIdentifier"); id != "" {
			args = append(args, "-activate", id)
		}
		err := n.Exec(tn, args...)
		if err == nil {
			return nil
		}
		// macOS can deny it notification permission; a banner from elsewhere beats none.
		n.Fallback = err
		if n.terminalNotifies() {
			return n.terminal(b)
		}
		return n.osascript(b)
	case "osascript":
		return n.osascript(b)
	default:
		bin, err := n.LookPath("notify-send")
		if err != nil {
			return fmt.Errorf("desktop banner: no notify-send on PATH")
		}
		body := b.Message
		if b.Subtitle != "" {
			body = b.Subtitle + "\n" + body
		}
		return n.Exec(bin, "--app-name=naviClaude", "--", b.Title, body)
	}
}

// focusCommand is the shell command a clicked banner runs: show naviClaude with
// pane's session selected, or (click "pane") the pane itself. It runs with
// launchd's bare environment, so binaries and the tmux socket are spelled out.
func (n *Notifier) focusCommand(pane string) string {
	if pane == "" {
		return ""
	}
	tmuxBin, err := n.LookPath("tmux")
	if err != nil {
		return ""
	}
	sock, _, _ := strings.Cut(n.Getenv("TMUX"), ",")
	if n.Click != ClickPane && n.Exe != "" {
		cmd := "PATH=" + shellQuote(filepath.Dir(tmuxBin)) + `:"$PATH" ` + shellQuote(n.Exe) + " focus " + shellQuote(pane)
		if sock != "" {
			cmd += " -S " + shellQuote(sock)
		}
		return cmd
	}
	cmd := shellQuote(tmuxBin)
	if sock != "" {
		cmd += " -S " + shellQuote(sock)
	}
	t := shellQuote(pane)
	return cmd + ` switch-client -t ` + t + ` \; select-window -t ` + t + ` \; select-pane -t ` + t
}

// terminal emits OSC 9 wrapped for tmux passthrough into the pane each attached
// client shows: that pane is visible, so allow-passthrough "on" lets it through.
func (n *Notifier) terminal(b Banner) error {
	// OSC 9 carries one line, so the title and subtitle lead the message.
	head := b.Title
	if b.Subtitle != "" {
		head = strings.TrimSpace(head + " (" + b.Subtitle + ")")
	}
	text := b.Message
	if head != "" {
		text = head + ": " + b.Message
	}
	seq := "\x1b]9;" + text + "\a"
	if n.Getenv("TMUX") == "" {
		return n.WriteTTY("/dev/tty", []byte(seq))
	}
	ttys, err := n.Clients()
	if err != nil {
		return err
	}
	wrapped := []byte(tmuxPassthrough(seq))
	var firstErr error
	for _, tty := range ttys {
		if err := n.WriteTTY(tty, wrapped); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// tmuxPassthrough wraps seq in tmux's DCS passthrough, doubling inner escapes.
func tmuxPassthrough(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// clean drops control characters (ESC, BEL, newlines) that would end or break
// an escape sequence or a notifier argument.
func clean(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
}

// tnEscape guards text terminal-notifier would otherwise parse as an option.
func tnEscape(s string) string {
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "[") {
		return `\` + s
	}
	return s
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func clientPaneTTYs() ([]string, error) {
	out, err := exec.Command("tmux", "list-clients", "-F", "#{pane_tty}").Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-clients: %w", err)
	}
	seen := map[string]bool{}
	var ttys []string
	for _, t := range strings.Fields(string(out)) {
		if !seen[t] {
			seen[t] = true
			ttys = append(ttys, t)
		}
	}
	return ttys, nil
}

func writeTTY(tty string, data []byte) error {
	f, err := os.OpenFile(tty, os.O_WRONLY|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0) // never stall the hook on a busy tty
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func runWait(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), bannerTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// startDetached starts the command without waiting; the reaper kills it after
// bannerTimeout if this process is still alive by then.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	timer := time.AfterFunc(bannerTimeout, func() { _ = cmd.Process.Kill() })
	go func() {
		_ = cmd.Wait()
		timer.Stop()
	}()
	return nil
}

// SelfExecutable is the absolute path this process was started by; hooks run it
// by the path in settings.json, which survives upgrades where os.Executable may not.
func SelfExecutable() (string, error) {
	if exe := os.Args[0]; filepath.IsAbs(exe) {
		return exe, nil
	}
	return os.Executable()
}
