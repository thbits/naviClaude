package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// privateTmux points tmux at a throwaway server for this test and returns a
// runner for it plus a short scratch dir; it skips when tmux is missing.
func privateTmux(t *testing.T) (func(args ...string) string, string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "nvt") // t.TempDir is too long for a socket path
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
	return tm, dir
}

// TestFormatQueriesOnRealTmux runs the FieldSep-joined queries against a real
// server, since some tmux versions rewrite control characters in output.
func TestFormatQueriesOnRealTmux(t *testing.T) {
	tm, dir := privateTmux(t)
	cwd := filepath.Join(dir, `a b\c`)
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	tm("-f", "/dev/null", "new-session", "-d", "-s", "my sess", "-x", "90", "-y", "30", "-c", cwd, "sleep 60")
	tm("set-option", "-p", "-t", "my sess:0.0", "@naviclaude_attn", "waiting")

	c := &Client{}
	panes, err := c.ListPanes()
	if err != nil || len(panes) != 1 {
		t.Fatalf("ListPanes = %+v, %v; want one pane", panes, err)
	}
	p := panes[0]
	if p.SessionName != "my sess" || p.Target != "my sess:0.0" || p.Attention != "waiting" || !strings.HasPrefix(p.PaneID, "%") {
		t.Errorf("pane = %+v", p)
	}
	if got, _ := filepath.EvalSymlinks(p.CurrentPath); got != mustEval(t, cwd) {
		t.Errorf("CurrentPath = %q, want %q", p.CurrentPath, cwd)
	}

	info, err := c.QueryServer(LaunchTarget{Pane: p.PaneID})
	if err != nil || info.WindowWidth != 90 || info.WindowHeight != 30 || info.Version == "" {
		t.Errorf("QueryServer = %+v, %v", info, err)
	}
	id, err := c.ServerIdentity()
	if err != nil || id.StartTime == 0 || !strings.HasPrefix(id.SocketPath, mustEval(t, dir)) {
		t.Errorf("ServerIdentity = %+v, %v; want a socket under %s", id, err, dir)
	}
	target, err := c.NewWindowPrint(NewWindowOptions{Target: "my sess:", Name: "w1", Command: "sleep 60"})
	if err != nil || target != "my sess:1.0" {
		t.Errorf("NewWindowPrint = %q, %v; want my sess:1.0", target, err)
	}
	if name := tm("display-message", "-p", "-t", "my sess:1", "#{window_name}"); name != "w1" {
		t.Errorf("new window name = %q, want w1", name)
	}
}

func mustEval(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
