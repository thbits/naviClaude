package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DiffStat holds a file's added/removed line counts as reported by git.
type DiffStat struct {
	Added   int
	Removed int
}

// GitDiffStats returns per-file line stats for the working tree of the git
// repository containing cwd, keyed by absolute file path. Tracked files report
// their net change versus the last commit (staged + unstaged, via
// `git diff --numstat HEAD`); untracked new files report all their lines as
// additions. It returns nil when cwd is empty or not inside a git repository,
// so callers can fall back to another source.
//
// Because it reflects the working tree, a file whose session edits were already
// committed (or reverted) simply has no entry -- there is no pending diff to
// show -- which is the intended, git-accurate behavior.
func GitDiffStats(cwd string) map[string]DiffStat {
	if cwd == "" {
		return nil
	}
	root, err := gitRoot(cwd)
	if err != nil || root == "" {
		return nil
	}

	stats := make(map[string]DiffStat)

	// Tracked changes (staged + unstaged) versus the last commit. This errors in
	// a repo with no commits yet; that is fine -- untracked files still count.
	if out, err := exec.Command("git", "-C", cwd, "diff", "--numstat", "HEAD").Output(); err == nil {
		parseNumstat(string(out), root, stats)
	}

	// Untracked new files: count every line as an addition (git would once added).
	if out, err := exec.Command("git", "-C", cwd, "ls-files", "--others", "--exclude-standard", "-z").Output(); err == nil {
		for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if rel == "" {
				continue
			}
			abs := filepath.Join(root, rel)
			if n := fileLineCount(abs); n > 0 {
				stats[abs] = DiffStat{Added: n}
			}
		}
	}

	return stats
}

// gitRoot returns the absolute path of the repository top-level containing cwd,
// or an error when cwd is not inside a git work tree.
func gitRoot(cwd string) (string, error) {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	// Canonicalize so keys match caller paths that resolve the same symlinks
	// (e.g. /var vs /private/var on macOS).
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root, nil
}

// parseNumstat parses `git diff --numstat` output into stats, resolving each
// repo-relative path against root to an absolute path. Binary files (reported
// as "-\t-") are skipped. Rename entries ("old => new") use the new path,
// best-effort; unusual quoted paths are left as-is.
func parseNumstat(out, root string, stats map[string]DiffStat) {
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		added, err1 := strconv.Atoi(fields[0])
		removed, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue // binary change ("-"), not a line count
		}
		path := fields[2]
		if idx := strings.Index(path, " => "); idx >= 0 {
			path = path[idx+len(" => "):]
		}
		stats[filepath.Join(root, path)] = DiffStat{Added: added, Removed: removed}
	}
}

// fileLineCount returns the number of lines in the file at path (0 on error).
func fileLineCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return len(splitLines(string(data)))
}
