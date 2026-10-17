package session

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// startReaped starts a command and reaps it in the background, as tmux or a
// shell would for a real agent, so a finished process doesn't linger as a zombie.
func startReaped(t *testing.T, script string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	t.Cleanup(func() { cmd.Process.Kill() })
	return cmd
}

func TestWaitForExit_WaitsForTermHandler(t *testing.T) {
	cmd := startReaped(t, `trap 'sleep 0.3; exit 0' TERM; while :; do sleep 0.05; done`)
	time.Sleep(100 * time.Millisecond) // let the trap install
	start := time.Now()
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if !waitForExit(cmd.Process.Pid, 5*time.Second) {
		t.Fatal("process did not exit")
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Errorf("returned after %v, before the TERM handler finished", elapsed)
	}
}

func TestWaitForExit_TimesOut(t *testing.T) {
	cmd := startReaped(t, `trap '' TERM; sleep 30`)
	time.Sleep(100 * time.Millisecond)
	syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
	if waitForExit(cmd.Process.Pid, 300*time.Millisecond) {
		t.Fatal("reported exit for a process that ignores SIGTERM")
	}
}
