package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// SessionInfo holds the data parsed from a single tmux list-sessions output
// line: the session name and its last-activity time.
type SessionInfo struct {
	Name     string
	Activity int64 // epoch seconds of last activity (session_activity)
}

// ListSessions returns live tmux sessions with their last activity; empty when
// tmux fails, so callers can still offer "create a new session".
func (c *Client) ListSessions() []SessionInfo {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name} #{session_activity}").Output()
	if err != nil {
		return nil
	}
	return ParseSessions(string(out))
}

// ParseSessions parses "<name> <activity>" lines, taking the last token as the
// activity so names with spaces survive; malformed lines are skipped.
func ParseSessions(raw string) []SessionInfo {
	var sessions []SessionInfo
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sep := strings.LastIndex(line, " ")
		if sep < 0 {
			continue
		}
		name := strings.TrimSpace(line[:sep])
		activity, err := strconv.ParseInt(strings.TrimSpace(line[sep+1:]), 10, 64)
		if name == "" || err != nil {
			continue
		}
		sessions = append(sessions, SessionInfo{Name: name, Activity: activity})
	}
	return sessions
}

// RenameSession renames tmux session oldName to newName, reporting tmux's own
// message (e.g. "duplicate session") on failure.
func (c *Client) RenameSession(oldName, newName string) error {
	return runReporting("rename-session", "-t", "="+oldName, newName)
}

// MoveWindow moves the window holding srcPane next to the window holding dstPane,
// before or after it, across sessions too. The moved window is not selected.
func (c *Client) MoveWindow(srcPane, dstPane string, before bool) error {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", dstPane, "#{window_id}").Output()
	if err != nil {
		return fmt.Errorf("tmux move-window: destination %s is gone", dstPane)
	}
	flag := "-a"
	if before {
		flag = "-b"
	}
	return runReporting("move-window", "-d", flag, "-s", srcPane, "-t", strings.TrimSpace(string(out)))
}

// MoveWindowToSession moves the window holding srcPane after the last window of
// session name, creating the session when missing. The moved window is not selected.
func (c *Client) MoveWindowToSession(srcPane, name string) error {
	out, err := exec.Command("tmux", "list-windows", "-t", "="+name+":", "-F", "#{window_id}").Output()
	if err != nil {
		return moveWindowToNewSession(srcPane, name)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return fmt.Errorf("tmux move-window: session %q has no windows", name)
	}
	return runReporting("move-window", "-d", "-a", "-s", srcPane, "-t", ids[len(ids)-1])
}

// moveWindowToNewSession creates session name and swaps the window holding
// srcPane in for the shell window tmux starts it with.
func moveWindowToNewSession(srcPane, name string) error {
	out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-P", "-F", "#{window_id}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session: %s", strings.TrimSpace(string(out)))
	}
	shell := strings.TrimSpace(string(out))
	if err := runReporting("move-window", "-a", "-s", srcPane, "-t", shell); err != nil {
		return err
	}
	return runReporting("kill-window", "-t", shell)
}

// runReporting runs a tmux command, folding its stderr into the error.
func runReporting(args ...string) error {
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("tmux %s: %s", args[0], msg)
		}
		return fmt.Errorf("tmux %s: %w", args[0], err)
	}
	return nil
}
