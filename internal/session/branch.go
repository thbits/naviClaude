package session

import (
	"context"
	"os/exec"
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

type branchEntry struct {
	branch string
	ok     bool
	at     time.Time
}

// branchCall is one git lookup that concurrent misses for its cwd wait on.
type branchCall struct {
	done   chan struct{}
	branch string
	ok     bool
}

var (
	branchMu       sync.Mutex
	branchCache    = map[string]branchEntry{}
	branchInflight = map[string]*branchCall{}
	// branchLookup is swapped in tests to avoid running git.
	branchLookup = gitCurrentBranch
)

// CurrentBranch returns the branch checked out in cwd, re-reading it from git
// at most every branchTTL. ok is false when cwd is not inside a git work tree.
func CurrentBranch(cwd string) (branch string, ok bool) {
	if cwd == "" {
		return "", false
	}
	branchMu.Lock()
	if e, hit := branchCache[cwd]; hit && time.Since(e.at) < branchTTL {
		branchMu.Unlock()
		return e.branch, e.ok
	}
	if c, busy := branchInflight[cwd]; busy {
		branchMu.Unlock()
		<-c.done
		return c.branch, c.ok
	}
	c := &branchCall{done: make(chan struct{})}
	branchInflight[cwd] = c
	branchMu.Unlock()

	c.branch, c.ok = branchLookup(cwd)

	branchMu.Lock()
	// Stamped after the lookup so a timed-out git backs off for a full TTL.
	branchCache[cwd] = branchEntry{branch: c.branch, ok: c.ok, at: time.Now()}
	delete(branchInflight, cwd)
	branchMu.Unlock()
	close(c.done)
	return c.branch, c.ok
}

// gitCurrentBranch reports "detached" for a detached HEAD, where git prints
// an empty branch name.
func gitCurrentBranch(cwd string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), branchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "branch", "--show-current")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	b := strings.TrimSpace(string(out))
	if b == "" {
		b = "detached"
	}
	return b, true
}
