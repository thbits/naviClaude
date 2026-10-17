package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

// paneFieldSep is the ASCII Unit Separator, which unlike a space can't occur in
// session names or paths; it must match the -F format in Client.ListPanes.
const paneFieldSep = "\x1f"

// PaneInfo holds the data parsed from a single tmux list-panes output line.
type PaneInfo struct {
	SessionName    string
	WindowIndex    int
	PaneIndex      int
	Target         string // "session:window.pane"
	CurrentCommand string
	PID            int
	CurrentPath    string
	PaneID         string // stable "%N" id
	Attention      string // @naviclaude_attn pane option set by the Claude hooks
}

// ParsePanes parses Client.ListPanes output, skipping malformed lines so one bad
// line doesn't abort the listing.
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

// parsePaneLine parses one list-panes line: target, command, pid, pane_id, attn and
// path joined by paneFieldSep, or the legacy four fields without pane_id and attn.
func parsePaneLine(line string) (PaneInfo, error) {
	// Capping the split keeps a stray separator inside the path.
	fields := strings.SplitN(line, paneFieldSep, 6)
	if len(fields) < 4 {
		return PaneInfo{}, fmt.Errorf("too few fields: %q", line)
	}

	targetStr := fields[0] // "session:window.pane"
	command := fields[1]
	pidStr := fields[2]
	// Six fields carry pane_id and the attention option before the path; fewer
	// is the legacy four-field form.
	var paneID, attn, path string
	if len(fields) == 6 && strings.HasPrefix(fields[3], "%") {
		paneID, attn, path = fields[3], fields[4], fields[5]
	} else {
		path = strings.Join(fields[3:], paneFieldSep)
	}

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
		PaneID:         paneID,
		Attention:      attn,
	}, nil
}

// CursorInfo holds a pane's cursor state parsed from a tmux display-message
// format string.
type CursorInfo struct {
	X          int  // cursor column (0-based)
	Y          int  // cursor row within the visible pane (0-based, top = 0)
	PaneHeight int  // number of visible rows in the pane
	Visible    bool // cursor_flag: whether the cursor is currently shown
}

// parseCursorInfo parses CursorPosition's space-separated cursor_x, cursor_y,
// pane_height and cursor_flag (1 when visible).
func parseCursorInfo(raw string) (CursorInfo, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 4 {
		return CursorInfo{}, fmt.Errorf("expected 4 cursor fields, got %q", raw)
	}
	x, err := strconv.Atoi(fields[0])
	if err != nil {
		return CursorInfo{}, fmt.Errorf("parse cursor_x %q: %w", fields[0], err)
	}
	y, err := strconv.Atoi(fields[1])
	if err != nil {
		return CursorInfo{}, fmt.Errorf("parse cursor_y %q: %w", fields[1], err)
	}
	h, err := strconv.Atoi(fields[2])
	if err != nil {
		return CursorInfo{}, fmt.Errorf("parse pane_height %q: %w", fields[2], err)
	}
	flag, err := strconv.Atoi(fields[3])
	if err != nil {
		return CursorInfo{}, fmt.Errorf("parse cursor_flag %q: %w", fields[3], err)
	}
	return CursorInfo{X: x, Y: y, PaneHeight: h, Visible: flag != 0}, nil
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
