package session

import (
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCurrentBranchCachesPerCWD(t *testing.T) {
	calls := 0
	orig := branchLookup
	branchLookup = func(cwd string) (string, bool) {
		calls++
		return "main", true
	}
	t.Cleanup(func() { branchLookup = orig })

	cwd := t.TempDir()
	for i := 0; i < 3; i++ {
		if b, ok := CurrentBranch(cwd); b != "main" || !ok {
			t.Fatalf("CurrentBranch = %q, %v", b, ok)
		}
	}
	if calls != 1 {
		t.Errorf("git lookups = %d, want 1 within the TTL", calls)
	}
	if _, ok := CurrentBranch(""); ok {
		t.Error("empty cwd should not be a repo")
	}
}

// Concurrent misses for one cwd share a single git run instead of each
// spawning their own.
func TestCurrentBranchDedupesConcurrentMisses(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	orig := branchLookup
	branchLookup = func(cwd string) (string, bool) {
		calls.Add(1)
		<-release
		return "feature", true
	}
	t.Cleanup(func() { branchLookup = orig })

	cwd := t.TempDir()
	const n = 8
	var wg sync.WaitGroup
	results := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, ok := CurrentBranch(cwd)
			if !ok {
				b = "!ok"
			}
			results[i] = b
		}(i)
	}
	// Let every goroutine reach the cache miss before the lookup returns.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("git lookups = %d, want 1 for concurrent misses", got)
	}
	for i, b := range results {
		if b != "feature" {
			t.Errorf("caller %d got %q, want feature", i, b)
		}
	}
}

func TestGitCurrentBranchTimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the git timeout")
	}
	dir := t.TempDir()
	// A fake git that never returns stands in for one stuck on a hung mount.
	script := "#!/bin/sh\nexec /bin/sleep 30\n"
	if err := os.WriteFile(dir+"/git", []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	start := time.Now()
	if _, ok := gitCurrentBranch(dir); ok {
		t.Error("a hung git must report not-a-repo")
	}
	if took := time.Since(start); took < branchTimeout/2 || took > branchTimeout+2*time.Second {
		t.Errorf("gitCurrentBranch took %v, want about %v", took, branchTimeout)
	}
}

func TestGitCurrentBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if _, ok := gitCurrentBranch(dir); ok {
		t.Fatal("a plain directory is not a git repo")
	}
	cmd := exec.Command("git", "init", "-q", "-b", "feature-x", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if err := cmd.Run(); err != nil {
		t.Skipf("git init: %v", err)
	}
	if b, ok := gitCurrentBranch(dir); !ok || b != "feature-x" {
		t.Errorf("gitCurrentBranch = %q, %v; want feature-x", b, ok)
	}
}
