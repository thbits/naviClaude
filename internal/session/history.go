package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HistoryScanner scans ~/.claude/projects/**/*.jsonl for closed sessions and
// reads ~/.claude/history.jsonl for session summaries.
type HistoryScanner struct {
	claudeDir string // defaults to ~/.claude
}

// NewHistoryScanner creates a HistoryScanner. If claudeDir is empty, it
// defaults to $HOME/.claude.
func NewHistoryScanner(claudeDir string) (*HistoryScanner, error) {
	if claudeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("history scanner: get home dir: %w", err)
		}
		claudeDir = filepath.Join(home, ".claude")
	}
	return &HistoryScanner{claudeDir: claudeDir}, nil
}

// HistoryEntry represents a single record from ~/.claude/history.jsonl.
type HistoryEntry struct {
	Display   string `json:"display"`
	SessionID string `json:"sessionId"`
	Project   string `json:"project"`
	Timestamp int64  `json:"timestamp"`
}

// LoadHistoryIndex reads all lines from history.jsonl and returns a map from
// sessionId to display text. If the file does not exist, an empty map is
// returned without error.
func (s *HistoryScanner) LoadHistoryIndex() (map[string]string, error) {
	path := filepath.Join(s.claudeDir, "history.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("open history.jsonl: %w", err)
	}
	defer f.Close()

	index := make(map[string]string)
	scanner := bufio.NewScanner(f)
	// history.jsonl can be large; increase buffer size.
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		var entry HistoryEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip unparseable lines silently
		}
		if entry.SessionID == "" {
			continue
		}
		// Keep first (earliest) entry per session so Summary = first prompt.
		if _, exists := index[entry.SessionID]; !exists {
			index[entry.SessionID] = entry.Display
		}
	}
	return index, scanner.Err()
}

// ScanClosed returns all closed sessions whose .jsonl file was modified within
// the last closedHours hours, cross-referenced against the provided set of
// active session IDs. Pass closedHours <= 0 to return all sessions (no time
// filter).
func (s *HistoryScanner) ScanClosed(closedHours float64, activeIDs map[string]bool) ([]*Session, error) {
	return s.scan(closedHours, activeIDs)
}

// ScanAll returns every closed session regardless of modification time.
func (s *HistoryScanner) ScanAll(activeIDs map[string]bool) ([]*Session, error) {
	return s.scan(0, activeIDs)
}

func (s *HistoryScanner) scan(closedHours float64, activeIDs map[string]bool) ([]*Session, error) {
	summaries, err := s.LoadHistoryIndex()
	if err != nil {
		// Non-fatal: proceed without summaries.
		summaries = make(map[string]string)
	}

	projectsDir := filepath.Join(s.claudeDir, "projects")
	pattern := filepath.Join(projectsDir, "*", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob project jsonl files: %w", err)
	}

	var cutoff time.Time
	if closedHours > 0 {
		cutoff = time.Now().Add(-time.Duration(closedHours * float64(time.Hour)))
	}

	var sessions []*Session
	for _, filePath := range files {
		info, err := os.Stat(filePath)
		if err != nil {
			continue
		}

		// Apply modification-time filter when requested.
		if !cutoff.IsZero() && info.ModTime().Before(cutoff) {
			continue
		}

		sess, err := parseSessionFile(filePath)
		if err != nil || sess == nil {
			continue
		}

		// Skip sessions that are currently active.
		if activeIDs[sess.ID] {
			continue
		}

		// Attach summary from history.jsonl if available.
		if display, ok := summaries[sess.ID]; ok {
			sess.Summary = display
		}

		sessions = append(sessions, sess)
	}
	return sessions, nil
}

// rawRecord is a loosely-typed representation of a single .jsonl line so we
// can handle polymorphic message.content (string or []interface{}).
type rawRecord struct {
	SessionID  string          `json:"sessionId"`
	Type       string          `json:"type"`
	Timestamp  string          `json:"timestamp"`
	CWD        string          `json:"cwd"`
	GitBranch  string          `json:"gitBranch"`
	Version    string          `json:"version"`
	Slug       string          `json:"slug"`
	Message    json.RawMessage `json:"message"`
}

// rawMessage holds only the fields we need from the message object.
type rawMessage struct {
	Model string `json:"model"`
	Role  string `json:"role"`
}

// parseSessionFile reads a .jsonl session file and constructs a Session from
// its records. Returns nil without error if the file is empty or yields no
// usable records.
func parseSessionFile(filePath string) (*Session, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// The session UUID is the filename stem (without extension).
	base := filepath.Base(filePath)
	sessionID := strings.TrimSuffix(base, ".jsonl")

	sess := &Session{
		ID:          sessionID,
		SessionFile: filePath,
		Status:      StatusClosed,
	}

	var (
		firstTime time.Time
		lastTime  time.Time
		modelSet  bool
	)

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rec rawRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // skip unparseable lines silently
		}

		// Parse timestamp.
		var recTime time.Time
		if rec.Timestamp != "" {
			recTime, _ = time.Parse(time.RFC3339Nano, rec.Timestamp)
			if recTime.IsZero() {
				recTime, _ = time.Parse(time.RFC3339, rec.Timestamp)
			}
		}
		if !recTime.IsZero() {
			if firstTime.IsZero() {
				firstTime = recTime
			}
			if recTime.After(lastTime) {
				lastTime = recTime
			}
		}

		// Pick up metadata from the first record that has it.
		if sess.CWD == "" && rec.CWD != "" {
			sess.CWD = rec.CWD
			sess.ProjectName = filepath.Base(rec.CWD)
		}
		if sess.GitBranch == "" && rec.GitBranch != "" {
			sess.GitBranch = rec.GitBranch
		}
		if sess.Version == "" && rec.Version != "" {
			sess.Version = rec.Version
		}
		if sess.Slug == "" && rec.Slug != "" {
			sess.Slug = rec.Slug
		}

		// Extract model from assistant records.
		if !modelSet && rec.Type == "assistant" && len(rec.Message) > 0 {
			var msg rawMessage
			if err := json.Unmarshal(rec.Message, &msg); err == nil && msg.Model != "" {
				sess.Model = classifyModel(msg.Model)
				modelSet = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// A file that produced no usable records (or had no session ID) is skipped.
	if sess.ID == "" {
		return nil, nil
	}

	if !lastTime.IsZero() {
		sess.LastActivity = lastTime
	} else {
		// Fall back to file modification time if no timestamps parsed.
		info, err := os.Stat(filePath)
		if err == nil {
			sess.LastActivity = info.ModTime()
		}
	}

	return sess, nil
}

// classifyModel maps a raw model ID like "claude-opus-4-6" to a short family
// name ("opus", "sonnet", or "haiku").
func classifyModel(modelID string) string {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "opus"):
		return "opus"
	case strings.Contains(lower, "sonnet"):
		return "sonnet"
	case strings.Contains(lower, "haiku"):
		return "haiku"
	default:
		return modelID
	}
}
