package attention

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// HookState summarizes how naviClaude's hooks look in settings.json.
type HookState int

const (
	HooksNotInstalled HookState = iota
	HooksCurrent
	HooksOutdated // older stamp, different events/matchers, or an unstable path
	HooksBroken   // the binary the hooks call is missing or not executable
)

// HookReport is the result of CheckHooks.
type HookReport struct {
	State    HookState
	Events   []string // events carrying a naviClaude hook, sorted
	Command  string
	Exe      string // binary the hooks call
	Version  int    // lowest stamp found; 1 for unstamped entries
	Problems []string
}

// CheckHooks inspects the settings file at path. A missing file means not installed.
func CheckHooks(path string) (HookReport, error) {
	var r HookReport
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r, nil
	} else if err != nil {
		return r, err
	}
	var s struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	type group struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	have := map[string]string{} // event -> matcher of naviClaude's group
	commands := map[string]bool{}
	events := make([]string, 0, len(s.Hooks))
	for ev := range s.Hooks {
		events = append(events, ev)
	}
	sort.Strings(events)
	for _, ev := range events {
		var groups []group
		if json.Unmarshal(s.Hooks[ev], &groups) != nil {
			continue
		}
		for _, g := range groups {
			for _, h := range g.Hooks {
				if !strings.Contains(h.Command, hookMarker) {
					continue
				}
				if _, dup := have[ev]; !dup {
					r.Events = append(r.Events, ev)
				}
				have[ev] = g.Matcher
				commands[h.Command] = true
				r.Command = h.Command
				if v := commandVersion(h.Command); r.Version == 0 || v < r.Version {
					r.Version = v
				}
			}
		}
	}
	if len(r.Events) == 0 {
		return r, nil
	}

	r.State = HooksCurrent
	if r.Version < HookVersion {
		r.Problems = append(r.Problems, fmt.Sprintf("hooks are v%d, this naviclaude installs v%d", r.Version, HookVersion))
	}
	for _, h := range hookEvents {
		m, ok := have[h.Event]
		switch {
		case !ok:
			r.Problems = append(r.Problems, "missing hook for "+h.Event)
		case m != h.Matcher:
			r.Problems = append(r.Problems, fmt.Sprintf("%s matcher is %q, want %q", h.Event, m, h.Matcher))
		}
		delete(have, h.Event)
	}
	for _, ev := range r.Events {
		if _, extra := have[ev]; extra {
			r.Problems = append(r.Problems, "unexpected hook for "+ev)
		}
	}
	if len(commands) > 1 {
		r.Problems = append(r.Problems, "hooks call different commands")
	}
	if len(r.Problems) > 0 {
		r.State = HooksOutdated
	}

	r.Exe = commandExe(r.Command)
	// Only worth flagging when a reinstall would store a stable symlink instead.
	if strings.Contains(r.Exe, "/Cellar/") && StableExecutable(r.Exe, exec.LookPath) != r.Exe {
		r.State = HooksOutdated
		r.Problems = append(r.Problems, "binary path is inside the Homebrew Cellar and breaks on upgrade")
	}
	if fi, err := os.Stat(r.Exe); r.Exe == "" || err != nil {
		r.State = HooksBroken
		r.Problems = append(r.Problems, "binary not found: "+r.Exe)
	} else if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		r.State = HooksBroken
		r.Problems = append(r.Problems, "binary not executable: "+r.Exe)
	}
	return r, nil
}

// commandVersion reads the stamp after hookMarker; unstamped entries are v1.
func commandVersion(cmd string) int {
	_, rest, _ := strings.Cut(cmd, hookMarker)
	if v, ok := strings.CutPrefix(strings.TrimSpace(rest), "v"); ok {
		v, _, _ = strings.Cut(v, " ")
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

// commandExe undoes HookCommand's single-quoting to recover the binary path.
func commandExe(cmd string) string {
	if !strings.HasPrefix(cmd, "'") {
		exe, _, _ := strings.Cut(cmd, " ")
		return exe
	}
	var b strings.Builder
	rest := cmd[1:]
	for {
		i := strings.IndexByte(rest, '\'')
		if i < 0 {
			return ""
		}
		b.WriteString(rest[:i])
		rest = rest[i+1:]
		if !strings.HasPrefix(rest, `\''`) {
			return b.String()
		}
		b.WriteByte('\'')
		rest = rest[3:]
	}
}

// StableExecutable picks the path hooks should store for exe: a PATH or Homebrew
// symlink to the same file, so an upgrade that moves the Cellar copy keeps working.
func StableExecutable(exe string, lookPath func(string) (string, error)) string {
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe
	}
	var candidates []string
	if p, err := lookPath("naviclaude"); err == nil {
		candidates = append(candidates, p)
	}
	if i := strings.Index(exe, "/Cellar/"); i > 0 {
		prefix := exe[:i]
		candidates = append(candidates, filepath.Join(prefix, "bin", "naviclaude"), filepath.Join(prefix, "opt", "naviclaude", "bin", "naviclaude"))
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil || strings.Contains(abs, "/Cellar/") {
			continue
		}
		if r, err := filepath.EvalSymlinks(abs); err == nil && r == real {
			return abs
		}
	}
	return exe
}
