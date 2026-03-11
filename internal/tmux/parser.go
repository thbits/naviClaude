package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

// PaneInfo holds the data parsed from a single tmux list-panes output line.
type PaneInfo struct {
	SessionName    string
	WindowIndex    int
	PaneIndex      int
	Target         string // "session:window.pane"
	CurrentCommand string
	PID            int
	CurrentPath    string
}

// ParsePanes parses the raw output of:
//
//	tmux list-panes -a -F '#{session_name}:#{window_index}.#{pane_index} #{pane_current_command} #{pane_pid} #{pane_current_path}'
//
// Each non-empty line is parsed into a PaneInfo.  Malformed lines are skipped
// with no error so that a single bad line does not abort the full listing.
func ParsePanes(raw string) ([]PaneInfo, error) {
	lines := strings.Split(raw, "\n")
	panes := make([]PaneInfo, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p, err := parsePaneLine(line)
		if err != nil {
			// skip malformed lines
			continue
		}
		panes = append(panes, p)
	}
	return panes, nil
}

// parsePaneLine parses a single line produced by the list-panes format string.
// Expected format: "session:window.pane command pid /current/path"
// Fields are space-separated; the path is everything after the pid field.
func parsePaneLine(line string) (PaneInfo, error) {
	// The format is:
	//   <session>:<window>.<pane> <command> <pid> <path>
	// There are exactly 4 space-separated tokens, but the path may itself
	// contain spaces.  We split into at most 4 fields so the path is preserved.
	fields := strings.SplitN(line, " ", 4)
	if len(fields) < 4 {
		return PaneInfo{}, fmt.Errorf("too few fields: %q", line)
	}

	targetStr := fields[0] // "session:window.pane"
	command := fields[1]
	pidStr := fields[2]
	path := fields[3]

	session, winIdx, paneIdx, err := parseTarget(targetStr)
	if err != nil {
		return PaneInfo{}, fmt.Errorf("parse target %q: %w", targetStr, err)
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return PaneInfo{}, fmt.Errorf("parse pid %q: %w", pidStr, err)
	}

	return PaneInfo{
		SessionName:    session,
		WindowIndex:    winIdx,
		PaneIndex:      paneIdx,
		Target:         targetStr,
		CurrentCommand: command,
		PID:            pid,
		CurrentPath:    path,
	}, nil
}

// parseTarget splits a tmux target string of the form "session:window.pane"
// into its components.
func parseTarget(target string) (session string, windowIndex, paneIndex int, err error) {
	// Split on the first ':' to get session and "window.pane"
	colonIdx := strings.Index(target, ":")
	if colonIdx < 0 {
		return "", 0, 0, fmt.Errorf("missing ':' in target")
	}
	session = target[:colonIdx]
	rest := target[colonIdx+1:]

	// Split on '.' to get window and pane indices
	dotIdx := strings.Index(rest, ".")
	if dotIdx < 0 {
		return "", 0, 0, fmt.Errorf("missing '.' in window.pane part")
	}

	windowIndex, err = strconv.Atoi(rest[:dotIdx])
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid window index %q: %w", rest[:dotIdx], err)
	}

	paneIndex, err = strconv.Atoi(rest[dotIdx+1:])
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid pane index %q: %w", rest[dotIdx+1:], err)
	}

	return session, windowIndex, paneIndex, nil
}
