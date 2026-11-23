package notify

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

// fakeNotifier records commands and tty writes instead of posting banners.
func fakeNotifier(mode, goos string, onPath map[string]string, env map[string]string) (*Notifier, *[]call, map[string][]byte) {
	var calls []call
	writes := map[string][]byte{}
	n := &Notifier{
		Mode: mode,
		GOOS: goos,
		LookPath: func(f string) (string, error) {
			if p, ok := onPath[f]; ok {
				return p, nil
			}
			return "", exec.ErrNotFound
		},
		Getenv:   func(k string) string { return env[k] },
		Exec:     func(name string, args ...string) error { calls = append(calls, call{name, args}); return nil },
		Clients:  func() ([]string, error) { return []string{"/dev/ttys001", "/dev/ttys002"}, nil },
		WriteTTY: func(tty string, data []byte) error { writes[tty] = data; return nil },
	}
	return n, &calls, writes
}

func TestTerminalNotifierFocusesPaneOnClick(t *testing.T) {
	n, calls, _ := fakeNotifier(DesktopSystem, "darwin",
		map[string]string{"terminal-notifier": "/opt/homebrew/bin/terminal-notifier", "tmux": "/opt/homebrew/bin/tmux"},
		map[string]string{"TMUX": "/tmp/tmux 501/default,123,0", "__CFBundleIdentifier": "com.mitchellh.ghostty"})
	if err := n.Notify(Banner{Title: "-proj", Message: "needs approval: Edit", Pane: "%12"}); err != nil {
		t.Fatal(err)
	}
	want := call{"/opt/homebrew/bin/terminal-notifier", []string{
		"-title", `\-proj`, "-message", "needs approval: Edit", "-group", "naviclaude-%12",
		"-execute", `'/opt/homebrew/bin/tmux' -S '/tmp/tmux 501/default' switch-client -t '%12' \; select-window -t '%12' \; select-pane -t '%12'`,
		"-activate", "com.mitchellh.ghostty",
	}}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("calls = %#v\nwant %#v", *calls, want)
	}
}

func TestOsascriptPassesTextAsArgv(t *testing.T) {
	n, calls, _ := fakeNotifier(DesktopSystem, "darwin", nil, nil)
	if err := n.Notify(Banner{Title: `my "proj"\`, Subtitle: "repo ⎇ main", Message: "line1\nline2\x1b]0;x\a", Pane: "%1"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].name != "osascript" {
		t.Fatalf("calls = %#v", *calls)
	}
	args := (*calls)[0].args
	if got := args[len(args)-4:]; !reflect.DeepEqual(got, []string{"--", `my "proj"\`, "line1 line2 ]0;x", "repo ⎇ main"}) {
		t.Errorf("argv tail = %q", got)
	}
	for _, a := range args {
		if strings.Contains(a, "proj") && strings.HasPrefix(a, "display") {
			t.Errorf("text leaked into the script: %q", a)
		}
	}
}

func TestOsascriptArgvReachesScript(t *testing.T) {
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("no osascript")
	}
	// Same argv shape as the banner, but returning the text instead of posting it.
	out, err := exec.Command("osascript", "-e", "on run argv", "-e", `return (item 1 of argv) & "|" & (item 2 of argv)`, "-e", "end run",
		"--", `-a "q"\`, "b").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != `-a "q"\|b` {
		t.Errorf("osascript saw %q", got)
	}
}

func TestTerminalModeWrapsOSC9ForTmux(t *testing.T) {
	n, calls, writes := fakeNotifier(DesktopTerminal, "darwin", nil, map[string]string{"TMUX": "/tmp/s,1,0"})
	if err := n.Notify(Banner{Title: "proj", Message: "finished\a"}); err != nil {
		t.Fatal(err)
	}
	want := "\x1bPtmux;\x1b\x1b]9;proj: finished\a\x1b\\"
	for _, tty := range []string{"/dev/ttys001", "/dev/ttys002"} {
		if string(writes[tty]) != want {
			t.Errorf("%s got %q, want %q", tty, writes[tty], want)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("terminal mode ran commands: %v", *calls)
	}
}

func TestTerminalModeOutsideTmuxWritesRaw(t *testing.T) {
	n, _, writes := fakeNotifier(DesktopTerminal, "darwin", nil, nil)
	if err := n.Notify(Banner{Message: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := string(writes["/dev/tty"]); got != "\x1b]9;hi\a" {
		t.Errorf("got %q", got)
	}
}

func TestOffAndDisabledDoNothing(t *testing.T) {
	n, calls, writes := fakeNotifier(DesktopOff, "darwin", nil, nil)
	_ = n.Notify(Banner{Message: "x"})
	m, calls2, _ := fakeNotifier(DesktopSystem, "darwin", nil, map[string]string{DisableBannerEnv: "1"})
	_ = m.Notify(Banner{Message: "x"})
	if len(*calls)+len(*calls2)+len(writes) != 0 {
		t.Errorf("expected no output, got %v %v %v", *calls, *calls2, writes)
	}
}

func TestLinuxSystemNeedsNotifySend(t *testing.T) {
	n, _, _ := fakeNotifier(DesktopSystem, "linux", nil, nil)
	if err := n.Notify(Banner{Message: "x"}); err == nil {
		t.Error("want an error without notify-send")
	}
	n, calls, _ := fakeNotifier(DesktopSystem, "linux", map[string]string{"notify-send": "/usr/bin/notify-send"}, nil)
	if err := n.Notify(Banner{Title: "t", Message: "m"}); err != nil || len(*calls) != 1 || (*calls)[0].name != "/usr/bin/notify-send" {
		t.Errorf("calls %v err %v", *calls, err)
	}
}

func TestTerminalReportsWriteErrors(t *testing.T) {
	n, _, _ := fakeNotifier(DesktopTerminal, "darwin", nil, map[string]string{"TMUX": "/tmp/s,1,0"})
	n.WriteTTY = func(string, []byte) error { return errors.New("boom") }
	if err := n.Notify(Banner{Message: "x"}); err == nil {
		t.Error("want error")
	}
}

func TestSetupHintPerBackend(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("not found") }
	found := func(f string) (string, error) { return "/opt/homebrew/bin/" + f, nil }
	n := &Notifier{Mode: DesktopSystem, GOOS: "darwin", LookPath: missing, Getenv: func(string) string { return "" }}
	if h := n.SetupHint(); !strings.Contains(h, "brew install terminal-notifier") {
		t.Errorf("osascript hint = %q, want the install advice", h)
	}
	n.LookPath = found
	if h := n.SetupHint(); !strings.Contains(h, "Alerts") {
		t.Errorf("terminal-notifier hint = %q, want the Alerts advice", h)
	}
	n.Mode = DesktopTerminal
	if h := n.SetupHint(); h != "" {
		t.Errorf("terminal hint = %q, want none", h)
	}
}

func TestSystemFallsBackToTerminalBannersInSupportedTerminals(t *testing.T) {
	env := map[string]string{"TMUX": "/tmp/tmux 501/default,123,0", "__CFBundleIdentifier": "com.mitchellh.ghostty"}
	n, calls, writes := fakeNotifier(DesktopSystem, "darwin", nil, env)
	n.Passthrough = func() bool { return true }
	if got := n.Backend(); got != "OSC 9" {
		t.Fatalf("Backend = %q, want OSC 9 in Ghostty without terminal-notifier", got)
	}
	if err := n.Notify(Banner{Title: "proj", Subtitle: "proj ⎇ main", Message: "Finished", Pane: "%1"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 || !strings.Contains(string(writes["/dev/ttys001"]), "proj (proj ⎇ main): Finished") {
		t.Errorf("calls = %v, writes = %q; want an OSC 9 write and no osascript", *calls, writes)
	}

	n.Passthrough = func() bool { return false } // tmux would drop it
	if got := n.Backend(); got != "osascript" {
		t.Errorf("Backend = %q without passthrough, want osascript", got)
	}
	env["__CFBundleIdentifier"] = "com.apple.Terminal"
	n.Passthrough = func() bool { return true }
	if got := n.Backend(); got != "osascript" {
		t.Errorf("Backend = %q in Terminal.app, want osascript", got)
	}
}

func TestTerminalNotifierDeniedFallsBack(t *testing.T) {
	env := map[string]string{"TMUX": "/tmp/tmux 501/default,123,0", "__CFBundleIdentifier": "com.mitchellh.ghostty"}
	n, calls, writes := fakeNotifier(DesktopSystem, "darwin",
		map[string]string{"terminal-notifier": "/opt/homebrew/bin/terminal-notifier"}, env)
	n.Exec = func(name string, args ...string) error {
		*calls = append(*calls, call{name, args})
		if strings.HasSuffix(name, "terminal-notifier") {
			return errors.New("Notifications are not allowed for this application")
		}
		return nil
	}
	n.Passthrough = func() bool { return true }
	if err := n.Notify(Banner{Title: "proj", Message: "Finished", Pane: "%1"}); err != nil {
		t.Fatalf("Notify = %v, want the fallback to succeed", err)
	}
	if n.Fallback == nil || len(writes["/dev/ttys001"]) == 0 {
		t.Errorf("fallback = %v, writes = %q; want the denial recorded and an OSC 9 banner", n.Fallback, writes)
	}

	n.Passthrough = func() bool { return false }
	*calls = nil
	if err := n.Notify(Banner{Title: "proj", Message: "Finished"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[1].name != "osascript" {
		t.Errorf("calls = %v, want terminal-notifier then osascript", *calls)
	}
}

func focusExec(t *testing.T, click string) string {
	t.Helper()
	n, calls, _ := fakeNotifier(DesktopSystem, "darwin",
		map[string]string{"terminal-notifier": "/opt/homebrew/bin/terminal-notifier", "tmux": "/opt/homebrew/bin/tmux"},
		map[string]string{"TMUX": "/tmp/tmux 501/default,123,0", "__CFBundleIdentifier": "com.mitchellh.ghostty"})
	n.Click, n.Exe = click, "/Users/me/it's/naviclaude"
	if err := n.Notify(Banner{Title: "p", Message: "m", Pane: "%12"}); err != nil {
		t.Fatal(err)
	}
	args := (*calls)[0].args
	for i, a := range args {
		if a == "-execute" {
			if args[len(args)-2] != "-activate" {
				t.Errorf("-activate must stay: %q", args)
			}
			return args[i+1]
		}
	}
	t.Fatalf("no -execute in %q", args)
	return ""
}

func TestClickNaviclaudeRunsFocusWithSocketAndAbsolutePaths(t *testing.T) {
	want := `PATH='/opt/homebrew/bin':"$PATH" '/Users/me/it'\''s/naviclaude' focus '%12' -S '/tmp/tmux 501/default'`
	for _, click := range []string{"", ClickNaviclaude} {
		if got := focusExec(t, click); got != want {
			t.Errorf("click %q: -execute = %s\nwant %s", click, got, want)
		}
	}
}

func TestClickPaneSwitchesStraightToPane(t *testing.T) {
	want := `'/opt/homebrew/bin/tmux' -S '/tmp/tmux 501/default' switch-client -t '%12' \; select-window -t '%12' \; select-pane -t '%12'`
	if got := focusExec(t, ClickPane); got != want {
		t.Errorf("-execute = %s\nwant %s", got, want)
	}
}

func TestClickNaviclaudeOmitsSocketOutsideTmux(t *testing.T) {
	n := &Notifier{Exe: "/bin/naviclaude", LookPath: func(string) (string, error) { return "/usr/bin/tmux", nil },
		Getenv: func(string) string { return "" }}
	if got, want := n.focusCommand("%1"), `PATH='/usr/bin':"$PATH" '/bin/naviclaude' focus '%1'`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
