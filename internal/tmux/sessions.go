package tmux

import (
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

// ListSessions returns every live tmux session with its last-activity time.
// Returns an empty slice (no error) when no sessions exist or tmux fails, so
// callers can still offer "create a new session".
func (c *Client) ListSessions() []SessionInfo {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name} #{session_activity}").Output()
	if err != nil {
		return nil
	}
	return ParseSessions(string(out))
}

// ParseSessions parses the raw output of:
//
//	tmux list-sessions -F '#{session_name} #{session_activity}'
//
// Each non-empty line is "<name> <activity>". The activity is the final
// space-separated token, so a name containing spaces is preserved. Lines with
// no activity token or a non-numeric activity are skipped silently so one bad
// line does not abort the listing.
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
