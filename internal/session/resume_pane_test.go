package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/tmux"
)

// TestResumeInSessionKeepsPaneWhenResumeFails runs a failing resume in an
// existing tmux session and expects its window to stay open as a shell.
func TestResumeInSessionKeepsPaneWhenResumeFails(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "nvr") // t.TempDir is too long for a socket path
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("SHELL", "/bin/sh")
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", args...).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() { tm("kill-server") })
	tm("-f", "/dev/null", "new-session", "-d", "-s", "work", "-x", "80", "-y", "24")
	tm("set-option", "-g", "default-shell", "/bin/sh")

	m := NewManager(&tmux.Client{}, "")
	sess := &Session{ID: "abc", CWD: dir, ProjectName: "p", Harness: "claude"}
	target, err := m.ResumeInSession(sess, "work", "false")
	if err != nil {
		t.Fatalf("ResumeInSession: %v", err)
	}
	time.Sleep(700 * time.Millisecond) // let the command run and fail
	panes := tm("list-panes", "-a", "-F", "#{session_name}:#{window_index}.#{pane_index} #{pane_dead}")
	if !strings.Contains("\n"+panes+"\n", "\n"+target+" 0\n") {
		t.Fatalf("pane %s gone after a failed resume; panes:\n%s", target, panes)
	}
}

// A shell whose startup reads stdin (an update prompt, `read` in an rc file) must
// not swallow the resume: it runs as the window's command, not typed keys.
func TestResumeInSessionSurvivesStartupPrompt(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "nvr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	// Interactive, it eats the first typed line; with -c it just runs the command.
	eater := filepath.Join(dir, "eatsh")
	script := "#!/bin/sh\nif [ \"$1\" = -c ]; then exec /bin/sh -c \"$2\"; fi\nread -r _ignored\nexec /bin/sh\n"
	if err := os.WriteFile(eater, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("SHELL", "/bin/sh")
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", args...).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() { tm("kill-server") })
	tm("-f", "/dev/null", "new-session", "-d", "-s", "work", "-x", "80", "-y", "24")
	tm("set-option", "-g", "default-shell", eater)

	marker := filepath.Join(dir, "resumed")
	m := NewManager(&tmux.Client{}, "")
	sess := &Session{ID: "abc", CWD: dir, ProjectName: "p", Harness: "claude"}
	target, err := m.ResumeInSession(sess, "work", "touch "+shellSingleQuote(marker))
	if err != nil {
		t.Fatalf("ResumeInSession: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("resume never ran (eaten by the shell's startup read?): %v", err)
	}
	if dead := tm("display-message", "-p", "-t", target, "#{pane_dead}"); dead != "0" {
		t.Errorf("pane %s dead=%q after the resume exited, want a live shell", target, dead)
	}
}

// The resumed agent runs under `sh -c`, which must still read as a plain shell
// parent so detection finds it and doesn't mark it a subprocess of another app.
func TestResumeInSessionAgentIsDetectable(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "nvr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("SHELL", "/bin/sh")
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", args...).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() { tm("kill-server") })
	tm("-f", "/dev/null", "new-session", "-d", "-s", "work", "-x", "80", "-y", "24")
	tm("set-option", "-g", "default-shell", "/bin/sh")

	m := NewManager(&tmux.Client{}, "")
	sess := &Session{ID: "abc", CWD: dir, ProjectName: "p", Harness: "claude"}
	target, err := m.ResumeInSession(sess, "work", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	panePID, _ := strconv.Atoi(tm("display-message", "-p", "-t", target, "#{pane_pid}"))
	isSleep := func(n string) bool { return n == "sleep" }
	var pid int
	for deadline := time.Now().Add(2 * time.Second); pid == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		tree := BuildProcessTree()
		if pid = tree.FindMatchingDescendant(panePID, isSleep, 6); pid == 0 && isSleep(tree.Name(panePID)) {
			pid = panePID
		}
		if pid != 0 {
			if app := tree.ParentAppName(pid); app != "" {
				t.Errorf("agent's parent reads as app %q, want a shell", app)
			}
		}
	}
	if pid == 0 {
		t.Fatalf("agent not found under pane pid %d (pane command %q)", panePID, tm("display-message", "-p", "-t", target, "#{pane_current_command}"))
	}
	t.Logf("pane_current_command=%q", tm("display-message", "-p", "-t", target, "#{pane_current_command}"))
}
