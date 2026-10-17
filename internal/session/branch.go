package session

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// branchTTL bounds how often CurrentBranch shells out to git for one cwd.
	branchTTL = 10 * time.Second
	// branchTimeout kills git on a cwd that hangs, e.g. a stale network mount.
	branchTimeout = 2 * time.Second
)

// GitRef is the repository a cwd belongs to and the branch checked out there.
type GitRef struct {
	Repo   string
	Branch string
}

type branchEntry struct {
	ref GitRef
	ok  bool
	at  time.Time
}

// branchCall is one git lookup that concurrent misses for its cwd wait on.
type branchCall struct {
	done chan struct{}
	ref  GitRef
	ok   bool
}

var (
	branchMu       sync.Mutex
	branchCache    = map[string]branchEntry{}
	branchInflight = map[string]*branchCall{}
	// branchLookup is swapped in tests to avoid running git.
	branchLookup = gitCurrentBranch
)

// CurrentBranch returns the repo and branch checked out in cwd, re-reading them
// from git at most every branchTTL. ok is false when cwd is not inside a git work tree.
func CurrentBranch(cwd string) (ref GitRef, ok bool) {
	if cwd == "" {
		return GitRef{}, false
	}
	branchMu.Lock()
	if e, hit := branchCache[cwd]; hit && time.Since(e.at) < branchTTL {
		branchMu.Unlock()
		return e.ref, e.ok
	}
	if c, busy := branchInflight[cwd]; busy {
		branchMu.Unlock()
		<-c.done
		return c.ref, c.ok
	}
	c := &branchCall{done: make(chan struct{})}
	branchInflight[cwd] = c
	branchMu.Unlock()

	c.ref, c.ok = branchLookup(cwd)

	branchMu.Lock()
	// Stamped after the lookup so a timed-out git backs off for a full TTL.
	branchCache[cwd] = branchEntry{ref: c.ref, ok: c.ok, at: time.Now()}
	delete(branchInflight, cwd)
	branchMu.Unlock()
	close(c.done)
	return c.ref, c.ok
}

// gitCurrentBranch reports "detached" for a detached HEAD, where git prints
// an empty branch name.
func gitCurrentBranch(cwd string) (GitRef, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), branchTimeout)
	defer cancel()
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
		cmd.WaitDelay = time.Second
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	b, err := git("branch", "--show-current")
	if err != nil {
		return GitRef{}, false
	}
	if b == "" {
		b = "detached"
	}
	ref := GitRef{Branch: b}
	if common, err := git("rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && common != "" {
		ref.Repo = repoName(common)
	}
	return ref, true
}

// repoName names a repo by its common git dir, so a linked worktree reports
// the main repo rather than its own directory.
func repoName(commonDir string) string {
	commonDir = filepath.Clean(commonDir)
	if filepath.Base(commonDir) == ".git" {
		return filepath.Base(filepath.Dir(commonDir))
	}
	return strings.TrimSuffix(filepath.Base(commonDir), ".git")
}
