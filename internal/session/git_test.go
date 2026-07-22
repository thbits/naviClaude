package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInit sets up a temp git repo with a single committed file and returns the
// repo root. It skips the test if git is not installed.
func gitInit(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

func TestGitDiffStats(t *testing.T) {
	dir := gitInit(t)

	// Modify the tracked file: replace one line, add one -> +2 -1 net vs HEAD.
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("a\nB\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a new untracked file: all lines counted as additions.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\ny\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := GitDiffStats(dir)
	if stats == nil {
		t.Fatal("GitDiffStats returned nil for a git repo")
	}

	// Keys are canonicalized (git reports the physical repo root), so resolve
	// the temp dir's symlinks to build the expected keys.
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	tracked := stats[filepath.Join(root, "tracked.txt")]
	if tracked.Added != 2 || tracked.Removed != 1 {
		t.Errorf("tracked.txt = +%d -%d, want +2 -1", tracked.Added, tracked.Removed)
	}

	newFile := stats[filepath.Join(root, "new.txt")]
	if newFile.Added != 2 || newFile.Removed != 0 {
		t.Errorf("new.txt = +%d -%d, want +2 -0", newFile.Added, newFile.Removed)
	}
}

func TestGitDiffStatsNonRepo(t *testing.T) {
	// A plain temp dir is not a git repo; expect nil so callers fall back.
	if stats := GitDiffStats(t.TempDir()); stats != nil {
		t.Errorf("GitDiffStats for non-repo = %v, want nil", stats)
	}
}

func TestGitDiffStatsEmptyCwd(t *testing.T) {
	if stats := GitDiffStats(""); stats != nil {
		t.Errorf("GitDiffStats(\"\") = %v, want nil", stats)
	}
}
