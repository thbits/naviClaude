package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// upstreamTTL bounds staleness when a ref changes in a way the stamp misses
// (e.g. reftable, or a ref git rewrote within the same mtime tick).
const upstreamTTL = 60 * time.Second

type upstreamEntry struct {
	ahead, behind int
	upstream      string // full upstream ref, e.g. refs/remotes/origin/main
	stamp         string
	at            time.Time
}

type upstreamCall struct {
	done          chan struct{}
	ahead, behind int
}

var (
	upstreamMu       sync.Mutex
	upstreamCache    = map[string]upstreamEntry{}
	upstreamInflight = map[string]*upstreamCall{}
	// upstreamGit is swapped in tests to count git runs.
	upstreamGit = runGit
)

// AheadBehind returns how many commits branch is ahead of and behind its
// upstream, cached per checkout (gitDir) until one of its refs changes; never fetches.
func AheadBehind(ctx context.Context, cwd, gitDir, commonDir, branch string) (ahead, behind int) {
	upstreamMu.Lock()
	e, hit := upstreamCache[gitDir]
	upstreamMu.Unlock()
	// Stat outside the lock: a hung mount must not stall other repos.
	if hit && time.Since(e.at) < upstreamTTL && refStamp(gitDir, commonDir, branch, e.upstream) == e.stamp {
		return e.ahead, e.behind
	}
	upstreamMu.Lock()
	if c, busy := upstreamInflight[gitDir]; busy {
		upstreamMu.Unlock()
		<-c.done
		return c.ahead, c.behind
	}
	c := &upstreamCall{done: make(chan struct{})}
	upstreamInflight[gitDir] = c
	upstreamMu.Unlock()

	// Stat before git reads the refs: one that moves meanwhile then misses the cache.
	stamp := refStamp(gitDir, commonDir, branch, "")
	var up string
	ok := true
	if out, err := upstreamGit(ctx, cwd, "rev-parse", "--symbolic-full-name", "@{upstream}"); err == nil {
		up = out
		stamp += stampFiles(upstreamRefFile(commonDir, up))
		if out, err := upstreamGit(ctx, cwd, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
			c.ahead, c.behind = parseLeftRight(out)
		} else {
			ok = false
		}
	} else if !gitAnswered(ctx, err) {
		ok = false
	}

	upstreamMu.Lock()
	if ok {
		upstreamCache[gitDir] = upstreamEntry{ahead: c.ahead, behind: c.behind, upstream: up, stamp: stamp, at: time.Now()}
	}
	delete(upstreamInflight, gitDir)
	upstreamMu.Unlock()
	close(c.done)
	return c.ahead, c.behind
}

// refStamp fingerprints the files a commit, fetch, push or upstream change
// rewrites, so a cached count is reused only while all of them are untouched.
func refStamp(gitDir, commonDir, branch, upstream string) string {
	files := []string{
		filepath.Join(gitDir, "HEAD"),
		filepath.Join(commonDir, "refs", "heads", filepath.FromSlash(branch)),
		filepath.Join(commonDir, "packed-refs"),
		filepath.Join(commonDir, "FETCH_HEAD"),
		filepath.Join(commonDir, "config"),
	}
	if upstream != "" {
		files = append(files, upstreamRefFile(commonDir, upstream))
	}
	return stampFiles(files...)
}

// upstreamRefFile is the loose ref file of a full upstream ref name.
func upstreamRefFile(commonDir, upstream string) string {
	return filepath.Join(commonDir, filepath.FromSlash(upstream))
}

// gitAnswered reports whether git ran to a verdict (e.g. "no upstream") rather
// than timing out or failing to start; only a verdict is worth caching.
func gitAnswered(ctx context.Context, err error) bool {
	var exit *exec.ExitError
	return ctx.Err() == nil && errors.As(err, &exit)
}

// stampFiles joins each file's mtime and size; a missing file still takes its slot.
func stampFiles(files ...string) string {
	var b strings.Builder
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			b.WriteString(strconv.FormatInt(fi.ModTime().UnixNano(), 36))
			b.WriteByte('.')
			b.WriteString(strconv.FormatInt(fi.Size(), 36))
		}
		b.WriteByte('|')
	}
	return b.String()
}

// parseLeftRight reads `rev-list --left-right --count` output: "ahead\tbehind".
func parseLeftRight(out string) (ahead, behind int) {
	f := strings.Fields(out)
	if len(f) != 2 {
		return 0, 0
	}
	a, err1 := strconv.Atoi(f[0])
	b, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return a, b
}

func runGit(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
