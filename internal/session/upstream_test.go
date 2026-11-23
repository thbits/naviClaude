package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// gitT runs git in dir with the user's global config shut out.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	gitT(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
}

// newTracked makes origin (a bare repo) and a clone on main tracking origin/main.
func newTracked(t *testing.T) (clone, origin string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	origin = filepath.Join(dir, "origin.git")
	seed := filepath.Join(dir, "seed")
	clone = filepath.Join(dir, "proj")
	gitT(t, dir, "init", "-q", "--bare", "-b", "main", origin)
	gitT(t, dir, "init", "-q", "-b", "main", seed)
	commit(t, seed, "c1")
	gitT(t, seed, "push", "-q", origin, "main")
	gitT(t, dir, "clone", "-q", origin, clone)
	gitT(t, seed, "remote", "add", "origin", origin)
	return clone, seed
}

func TestGitCurrentBranchAheadBehind(t *testing.T) {
	clone, seed := newTracked(t)

	if r, ok := gitCurrentBranch(clone); !ok || r != (GitRef{Repo: "proj", Branch: "main"}) {
		t.Fatalf("in sync: %+v, %v", r, ok)
	}

	commit(t, clone, "local1")
	commit(t, clone, "local2")
	if r, _ := gitCurrentBranch(clone); r.Ahead != 2 || r.Behind != 0 {
		t.Errorf("after 2 local commits: %+v, want ↑2", r)
	}

	commit(t, seed, "remote1")
	gitT(t, seed, "push", "-q", "origin", "main")
	gitT(t, clone, "fetch", "-q")
	if r, _ := gitCurrentBranch(clone); r.Ahead != 2 || r.Behind != 1 {
		t.Errorf("after fetch: %+v, want ↑2↓1", r)
	}

	// Detached HEAD and a branch without upstream show no arrows.
	gitT(t, clone, "checkout", "-q", "--detach")
	if r, _ := gitCurrentBranch(clone); r.Branch != "detached" || r.Ahead != 0 || r.Behind != 0 {
		t.Errorf("detached: %+v", r)
	}
	gitT(t, clone, "checkout", "-q", "-b", "local-only")
	if r, _ := gitCurrentBranch(clone); r.Branch != "local-only" || r.Ahead != 0 || r.Behind != 0 {
		t.Errorf("no upstream: %+v", r)
	}
}

func TestGitCurrentBranchWorktreeAheadBehind(t *testing.T) {
	clone, _ := newTracked(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, clone, "worktree", "add", "-q", "-b", "feat", wt, "origin/main")
	commit(t, wt, "w1")
	r, ok := gitCurrentBranch(wt)
	if !ok || r.Repo != "proj" || r.Branch != "feat" || r.Ahead != 1 || r.Behind != 0 {
		t.Errorf("worktree: %+v, %v; want proj feat ↑1", r, ok)
	}
	// The main checkout keeps its own, separately cached count.
	if r, _ := gitCurrentBranch(clone); r.Branch != "main" || r.Ahead != 0 {
		t.Errorf("main checkout: %+v", r)
	}
}

// Many sessions in one checkout share one count until a ref changes.
func TestAheadBehindCachesPerCheckout(t *testing.T) {
	clone, _ := newTracked(t)
	var runs atomic.Int32
	orig := upstreamGit
	upstreamGit = func(ctx context.Context, cwd string, args ...string) (string, error) {
		runs.Add(1)
		return orig(ctx, cwd, args...)
	}
	t.Cleanup(func() { upstreamGit = orig })

	gitDir := gitT(t, clone, "rev-parse", "--path-format=absolute", "--git-dir")
	sub := filepath.Join(clone, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, cwd := range []string{clone, sub, clone} {
		AheadBehind(ctx, cwd, gitDir, gitDir, "main")
	}
	if got := runs.Load(); got != 2 { // rev-parse @{upstream} + rev-list, once
		t.Errorf("git runs = %d, want 2", got)
	}

	commit(t, clone, "local")
	if a, b := AheadBehind(ctx, sub, gitDir, gitDir, "main"); a != 1 || b != 0 {
		t.Errorf("after commit: ↑%d↓%d, want ↑1", a, b)
	}
}

func TestParseLeftRight(t *testing.T) {
	for in, want := range map[string][2]int{"2\t1": {2, 1}, "0\t0\n": {0, 0}, "": {0, 0}, "x\t1": {0, 0}} {
		if a, b := parseLeftRight(in); a != want[0] || b != want[1] {
			t.Errorf("parseLeftRight(%q) = %d,%d want %v", in, a, b, want)
		}
	}
}

// A ref that moves while git runs must not be cached under the newer stamp.
func TestAheadBehindStampPrecedesGit(t *testing.T) {
	clone, _ := newTracked(t)
	orig := upstreamGit
	raced := false
	upstreamGit = func(ctx context.Context, cwd string, args ...string) (string, error) {
		out, err := orig(ctx, cwd, args...)
		if args[0] == "rev-list" && !raced {
			raced = true
			commit(t, clone, "during count") // lands after git read the refs
		}
		return out, err
	}
	t.Cleanup(func() { upstreamGit = orig })

	gitDir := gitT(t, clone, "rev-parse", "--path-format=absolute", "--git-dir")
	ctx := context.Background()
	if a, _ := AheadBehind(ctx, clone, gitDir, gitDir, "main"); a != 0 {
		t.Fatalf("first count ↑%d, want the pre-commit ↑0", a)
	}
	if a, b := AheadBehind(ctx, clone, gitDir, gitDir, "main"); a != 1 || b != 0 {
		t.Errorf("after the racing commit: ↑%d↓%d, want ↑1 (stale count was cached)", a, b)
	}
}

// A timed-out or failed git run shows no arrows and is retried next time.
func TestAheadBehindDoesNotCacheFailures(t *testing.T) {
	clone, _ := newTracked(t)
	commit(t, clone, "local")
	orig := upstreamGit
	var runs atomic.Int32
	fail := true
	upstreamGit = func(ctx context.Context, cwd string, args ...string) (string, error) {
		runs.Add(1)
		if fail {
			return "", context.DeadlineExceeded
		}
		return orig(ctx, cwd, args...)
	}
	t.Cleanup(func() { upstreamGit = orig })

	gitDir := gitT(t, clone, "rev-parse", "--path-format=absolute", "--git-dir")
	ctx := context.Background()
	if a, b := AheadBehind(ctx, clone, gitDir, gitDir, "main"); a != 0 || b != 0 {
		t.Errorf("failed run: ↑%d↓%d, want no arrows", a, b)
	}
	fail = false
	if a, b := AheadBehind(ctx, clone, gitDir, gitDir, "main"); a != 1 || b != 0 {
		t.Errorf("retry: ↑%d↓%d after %d runs, want ↑1", a, b, runs.Load())
	}

	// A branch with no upstream is a real answer and stays cached.
	gitT(t, clone, "checkout", "-q", "-b", "local-only")
	runs.Store(0)
	for range 3 {
		AheadBehind(ctx, clone, gitDir, gitDir, "local-only")
	}
	if got := runs.Load(); got != 1 {
		t.Errorf("no-upstream git runs = %d, want 1", got)
	}
}
