package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/thbits/naviClaude/internal/session"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// HistoryScanner scans ~/.claude/projects/**/*.jsonl for closed sessions and
// reads ~/.claude/history.jsonl for session summaries.
type HistoryScanner struct {
	claudeDir string // defaults to ~/.claude

	// cacheMu serializes LoadHistoryIndex, which overlapping refresh Cmds call.
	cacheMu         sync.Mutex
	cachedIndex     map[string]string // cached history index (sessionID -> display)
	cachedIndexSize int64             // history.jsonl size and mtime the index was built from
	cachedIndexMod  time.Time

	// parseMu guards parsed; transcripts are re-parsed only when size or mtime change.
	parseMu sync.Mutex
	parsed  map[string]parsedTranscript
}

// parsedTranscript is a cached parseSessionFile result; sess is nil for files
// that yield no session.
type parsedTranscript struct {
	size    int64
	modTime time.Time
	sess    *session.Session
}

// parseCached returns a fresh copy of the file's parsed session, re-parsing
// only when the file changed since the cached parse.
func (s *HistoryScanner) parseCached(filePath string, info os.FileInfo, seen map[string]bool) *session.Session {
	seen[filePath] = true
	s.parseMu.Lock()
	entry, ok := s.parsed[filePath]
	s.parseMu.Unlock()
	if !ok || entry.size != info.Size() || !entry.modTime.Equal(info.ModTime()) {
		sess, err := parseSessionFile(filePath)
		if err != nil {
			return nil // transient read error: retry next scan
		}
		entry = parsedTranscript{size: info.Size(), modTime: info.ModTime(), sess: sess}
		s.parseMu.Lock()
		if s.parsed == nil {
			s.parsed = make(map[string]parsedTranscript)
		}
		s.parsed[filePath] = entry
		s.parseMu.Unlock()
	}
	if entry.sess == nil {
		return nil
	}
	// Callers mutate the session (summary, aliases), so never hand out the cached one.
	cp := *entry.sess
	return &cp
}

// pruneParsed drops cache entries for transcripts no longer on disk.
func (s *HistoryScanner) pruneParsed(seen map[string]bool) {
	s.parseMu.Lock()
	defer s.parseMu.Unlock()
	for path := range s.parsed {
		if !seen[path] {
			delete(s.parsed, path)
		}
	}
}

// transcriptID is the session ID parseSessionFile assigns: the filename stem.
func transcriptID(filePath string) string {
	return strings.TrimSuffix(filepath.Base(filePath), ".jsonl")
}

// NewHistoryScanner creates a HistoryScanner. If claudeDir is empty, it
// defaults to $HOME/.claude.
func NewHistoryScanner(claudeDir string) (*HistoryScanner, error) {
	if claudeDir == "" {
		base, err := claudeHomeDir()
		if err != nil {
			return nil, fmt.Errorf("history scanner: get home dir: %w", err)
		}
		claudeDir = base
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

// LoadHistoryIndex maps sessionId to its first history.jsonl prompt, re-parsing
// only when the file's size or mtime changed; a missing file yields an empty map.
func (s *HistoryScanner) LoadHistoryIndex() (map[string]string, error) {
	// A published map is never mutated, so callers may read it unlocked.
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	path := filepath.Join(s.claudeDir, "history.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("open history.jsonl: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat history.jsonl: %w", err)
	}
	if s.cachedIndex != nil && info.Size() == s.cachedIndexSize && info.ModTime().Equal(s.cachedIndexMod) {
		return s.cachedIndex, nil
	}

	index := make(map[string]string)
	scanner := bufio.NewScanner(f)
	// history.jsonl can be large; increase buffer size.
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, maxTranscriptLineBytes)

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

	if err := scanner.Err(); err != nil {
		return index, err
	}

	s.cachedIndex = index
	s.cachedIndexSize, s.cachedIndexMod = info.Size(), info.ModTime()
	return index, nil
}

// ScanClosed returns sessions not in activeIDs whose transcript changed in the
// last closedHours hours (no time filter when <= 0).
func (s *HistoryScanner) ScanClosed(closedHours float64, activeIDs map[string]bool) ([]*session.Session, error) {
	return s.scan(closedHours, activeIDs)
}

// ScanAll returns every closed session regardless of modification time.
func (s *HistoryScanner) ScanAll(activeIDs map[string]bool) ([]*session.Session, error) {
	return s.scan(0, activeIDs)
}

// ScanClosedAndAll returns ScanClosed and ScanAll from one glob-and-parse pass;
// closed is a subset of all sharing the same *Session pointers.
func (s *HistoryScanner) ScanClosedAndAll(closedHours float64, activeIDs map[string]bool) (closed []*session.Session, all []*session.Session, err error) {
	summaries, lerr := s.LoadHistoryIndex()
	if lerr != nil {
		// Non-fatal: proceed without summaries.
		summaries = make(map[string]string)
	}

	projectsDir := filepath.Join(s.claudeDir, "projects")
	pattern := filepath.Join(projectsDir, "*", "*.jsonl")
	files, gerr := filepath.Glob(pattern)
	if gerr != nil {
		return nil, nil, fmt.Errorf("glob project jsonl files: %w", gerr)
	}

	var cutoff time.Time
	if closedHours > 0 {
		cutoff = time.Now().Add(-time.Duration(closedHours * float64(time.Hour)))
	}

	seen := make(map[string]bool, len(files))
	for _, filePath := range files {
		// Live transcripts are the largest and change every turn; skip them unparsed.
		if activeIDs[transcriptID(filePath)] {
			seen[filePath] = true
			continue
		}
		info, serr := os.Stat(filePath)
		if serr != nil {
			continue
		}

		sess := s.parseCached(filePath, info, seen)
		if sess == nil {
			continue
		}

		// Attach summary from history.jsonl if available.
		if display, ok := summaries[sess.ID]; ok {
			sess.Summary = display
		}

		all = append(all, sess)

		// A zero cutoff (closedHours <= 0) means no time filter.
		if cutoff.IsZero() || !info.ModTime().Before(cutoff) {
			closed = append(closed, sess)
		}
	}
	s.pruneParsed(seen)
	return closed, all, nil
}

func (s *HistoryScanner) scan(closedHours float64, activeIDs map[string]bool) ([]*session.Session, error) {
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

	var sessions []*session.Session
	seen := make(map[string]bool, len(files))
	for _, filePath := range files {
		if activeIDs[transcriptID(filePath)] {
			continue
		}
		info, err := os.Stat(filePath)
		if err != nil {
			continue
		}

		// Apply modification-time filter when requested.
		if !cutoff.IsZero() && info.ModTime().Before(cutoff) {
			continue
		}

		sess := s.parseCached(filePath, info, seen)
		if sess == nil {
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
	SessionID string          `json:"sessionId"`
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Version   string          `json:"version"`
	Slug      string          `json:"slug"`
	AITitle   string          `json:"aiTitle"`
	Message   json.RawMessage `json:"message"`
}

// rawMessage holds only the fields we need from the message object.
type rawMessage struct {
	Model string `json:"model"`
	Role  string `json:"role"`
}

// maxTranscriptLineBytes is generous because pasted files and images make big
// records; readers sharing it agree on which sessions parse.
const maxTranscriptLineBytes = 4 * 1024 * 1024

// parseSessionFile builds a closed Session from a transcript; nil, nil when it
// yields no usable records.
func parseSessionFile(filePath string) (*session.Session, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sessionID := transcriptID(filePath)

	sess := &session.Session{
		ID:          sessionID,
		SessionFile: filePath,
		Status:      session.StatusClosed,
		Harness:     Name,
	}

	var (
		firstTime time.Time
		lastTime  time.Time
		modelSet  bool
		aiTitle   string
	)

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxTranscriptLineBytes)

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
		if rec.Type == "ai-title" && rec.AITitle != "" {
			aiTitle = rec.AITitle
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
	if err := scanner.Err(); err != nil && err != bufio.ErrTooLong {
		return nil, err
	}
	// ErrTooLong is not fatal: metadata from earlier lines still holds, so the
	// session stays in the picker if a cwd was seen before the huge record.

	// A file that produced no usable records, no session ID, or no CWD
	// (e.g. empty or metadata-only .jsonl) is skipped.
	if sess.ID == "" || sess.CWD == "" {
		return nil, nil
	}

	// A closed session keeps Claude's title instead of the Slug/ProjectName placeholder.
	if aiTitle != "" {
		sess.DisplayName = aiTitle
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
