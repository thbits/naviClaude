package attention

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestExecTmuxOnRealTmux reads pane and window state from a throwaway server,
// since some tmux versions rewrite control characters in output.
func TestExecTmuxOnRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "nva") // t.TempDir is too long for a socket path
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", dir)
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", args...).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() { tm("kill-server") })
	tm("-f", "/dev/null", "new-session", "-d", "-s", "my sess", "-n", "my win", "sleep 60")
	tm("set-option", "-p", "-t", "my sess:0.0", optAttn, "waiting")
	tm("set-option", "-p", "-t", "my sess:0.0", optBusy, "tok")
	tm("set-option", "-w", "-t", "my sess:0", optOrigName, "orig name")
	pane := tm("display-message", "-p", "-t", "my sess:0.0", "#{pane_id}")

	var x execTmux
	ps, err := x.Pane(pane)
	if err != nil || ps.Session != "my sess" || ps.Attn != "waiting" || ps.Busy != "tok" || !strings.HasPrefix(ps.Window, "@") {
		t.Errorf("Pane = %+v, %v", ps, err)
	}
	ws, err := x.Window(ps.Window)
	if err != nil || ws.Name != "my win" || ws.OrigName != "orig name" {
		t.Errorf("Window = %+v, %v", ws, err)
	}
	marks, err := x.WindowPanes(ps.Window)
	if err != nil || len(marks) != 1 || marks[0] != (paneMarks{Attn: "waiting", Busy: "tok"}) {
		t.Errorf("WindowPanes = %+v, %v", marks, err)
	}
}
