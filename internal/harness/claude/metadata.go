package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

// sessionFileModTime returns the modification time of a session's .jsonl file.
func sessionFileModTime(sessionID, cwd string) time.Time {
	path := SessionFilePath(sessionID, cwd)
	if path == "" {
		return time.Time{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// extractSessionIDFallback attempts to determine the session ID when the
// ~/.claude/sessions/{pid}.json metadata file is not available.
func extractSessionIDFallback(pid int, cwd string) string {
	// Try command line: "claude --resume <uuid>"
	cmdLine := fullCommandLine(pid)
	if id := parseResumeFlag(cmdLine); id != "" {
		return id
	}

	// Fall back to most recently modified .jsonl in the project dir.
	return findLatestSessionFile(cwd)
}

// sessionMetadata holds fields from ~/.claude/sessions/{pid}.json. Claude Code
// writes this file per running process and keeps Status live (Claude >= 2.1).
type sessionMetadata struct {
	SessionID       string `json:"sessionId"`
	Name            string `json:"name"`
	NameSource      string `json:"nameSource"`      // "derived" = auto placeholder
	Status          string `json:"status"`          // busy|waiting|idle|shell
	WaitingFor      string `json:"waitingFor"`      // why it is waiting, e.g. "permission prompt"
	StatusUpdatedAt int64  `json:"statusUpdatedAt"` // epoch ms
}

// readSessionMetadata reads the full metadata from ~/.claude/sessions/{pid}.json.
func readSessionMetadata(pid int) sessionMetadata {
	home, err := os.UserHomeDir()
	if err != nil {
		return sessionMetadata{}
	}
	path := filepath.Join(home, ".claude", "sessions", strconv.Itoa(pid)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return sessionMetadata{}
	}
	return parseSessionMetadata(data)
}

// parseSessionMetadata unmarshals session-metadata JSON, returning the zero
// struct (not an error) on malformed input. Split from readSessionMetadata so
// the parse + field handling can be unit-tested without touching the filesystem.
func parseSessionMetadata(data []byte) sessionMetadata {
	var meta sessionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return sessionMetadata{}
	}
	return meta
}

// mapNativeStatus maps Claude Code's own session status string (from the
// ~/.claude/sessions/<pid>.json "status" field) to a SessionStatus. The bool is
// false when the status is empty or unrecognized -- e.g. a Claude Code version
// predating the status field -- signalling the caller to fall back to
// signal-based detection. "shell" (idle with a foreground shell command open) is
// folded into idle.
func mapNativeStatus(status string) (session.SessionStatus, bool) {
	switch status {
	case "busy":
		return session.StatusActive, true
	case "waiting":
		return session.StatusWaiting, true
	case "idle", "shell":
		return session.StatusIdle, true
	default:
		return session.StatusIdle, false
	}
}

// readSessionMetadataByID scans ~/.claude/sessions and returns the full metadata
// of the file whose sessionId matches. This is robust to the PID naviClaude
// picks (via the comm=="claude" process-tree walk) not being the PID Claude
// wrote its <pid>.json under -- the CLI layers a launcher/wrapper process (e.g.
// comm "2.1.190") so the file may belong to a different node in the tree. The
// sessionId, by contrast, naviClaude resolves reliably. Returns (zero, false)
// when sessionID is empty or no matching file is found.
func readSessionMetadataByID(sessionID string) (sessionMetadata, bool) {
	if sessionID == "" {
		return sessionMetadata{}, false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return sessionMetadata{}, false
	}
	dir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return sessionMetadata{}, false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if m := parseSessionMetadata(data); m.SessionID == sessionID {
			return m, true
		}
	}
	return sessionMetadata{}, false
}

// readSessionStatusByID returns just the native status of the metadata file
// whose sessionId matches, or ("", false) when none is found. Thin wrapper over
// readSessionMetadataByID for the status-only callers.
func readSessionStatusByID(sessionID string) (string, bool) {
	m, ok := readSessionMetadataByID(sessionID)
	if !ok {
		return "", false
	}
	return m.Status, true
}

// nativeStatusString returns Claude Code's own status for a session, preferring
// the fast per-PID file read and falling back to a sessionId scan when the
// picked PID has no usable status (PID/launcher mismatch). Empty string means no
// native status is available (older Claude, or no matching file).
func nativeStatusString(pid int, sessionID string) string {
	if s := readSessionMetadata(pid).Status; s != "" {
		return s
	}
	s, _ := readSessionStatusByID(sessionID)
	return s
}

// NativeState is Claude Code's own UI state for a running session.
type NativeState struct {
	Status     string // busy|waiting|idle|shell
	WaitingFor string // set while Status is "waiting"
	UpdatedAt  int64  // epoch ms of the last status change
}

// ReadNativeState returns the session's native state, trying the per-PID file
// before a sessionId scan. ok is false when no file carries a status.
func ReadNativeState(pid int, sessionID string) (NativeState, bool) {
	m := readSessionMetadata(pid)
	if m.Status == "" || (sessionID != "" && m.SessionID != sessionID) {
		m, _ = readSessionMetadataByID(sessionID)
	}
	if m.Status == "" {
		return NativeState{}, false
	}
	return NativeState{Status: m.Status, WaitingFor: m.WaitingFor, UpdatedAt: m.StatusUpdatedAt}, true
}

// NativeStatus reads Claude Code's own status for a session and maps it to a
// SessionStatus. It tries the per-PID file first, then a sessionId scan. The
// bool is false when no usable native status is available (file missing, or a
// Claude version that does not write the status field), in which case callers
// should fall back to signal-based detection. This is the single source of truth
// shared by the detector loop and the preview path.
func NativeStatus(pid int, sessionID string) (session.SessionStatus, bool) {
	return mapNativeStatus(nativeStatusString(pid, sessionID))
}

// fullCommandLine returns the full command line of a process.
func fullCommandLine(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseResumeFlag extracts the session ID from a resume flag in a command line.
// It handles the long flag in inline (`--resume=<uuid>`) and space-separated
// (`--resume <uuid>`) forms, plus the documented short forms (`-r <uuid>` and
// `-r=<uuid>`).
//
// A resume flag with no value yields "" rather than mistaking a neighbouring
// token for a session ID: bare `--resume` (or `-r`) opens Claude's interactive
// session picker, and forms like `--resume --fork-session` or `--resume --model
// opus` carry the ID elsewhere (or not on the command line at all). The
// space-separated value is therefore accepted only when the next token does not
// itself start with '-'. A session UUID never starts with '-', so this never
// rejects a real ID.
func parseResumeFlag(cmdLine string) string {
	parts := strings.Fields(cmdLine)
	for i, p := range parts {
		switch {
		// Inline forms: --resume=<uuid> / -r=<uuid> (value is the rest of the token).
		case strings.HasPrefix(p, "--resume="):
			return strings.TrimPrefix(p, "--resume=")
		case strings.HasPrefix(p, "-r="):
			return strings.TrimPrefix(p, "-r=")
		// Space-separated forms: --resume <uuid> / -r <uuid>. Only consume the next
		// token as the value when it is not itself a flag.
		case p == "--resume" || p == "-r":
			if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "-") {
				return parts[i+1]
			}
		}
	}
	return ""
}

// findLatestSessionFile finds the most recently modified .jsonl file in the
// Claude projects directory that matches the given CWD, and returns its
// session UUID (filename stem).
func findLatestSessionFile(cwd string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// The path slug is the CWD with "/" replaced by "-".
	slug := cwdSlug(cwd)
	projectDir := filepath.Join(home, ".claude", "projects", slug)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return ""
	}

	type fileEntry struct {
		name    string
		modTime time.Time
	}

	var jsonlFiles []fileEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		jsonlFiles = append(jsonlFiles, fileEntry{
			name:    e.Name(),
			modTime: info.ModTime(),
		})
	}

	if len(jsonlFiles) == 0 {
		return ""
	}

	// Sort by modification time descending, pick the most recent.
	sort.Slice(jsonlFiles, func(i, j int) bool {
		return jsonlFiles[i].modTime.After(jsonlFiles[j].modTime)
	})

	return strings.TrimSuffix(jsonlFiles[0].name, ".jsonl")
}

// extractModelFromSessionFile reads the first few assistant messages from the
// session's .jsonl file to extract the model field. This allows active sessions
// to display their model (opus/sonnet/haiku) without waiting for the session
// to close.
func extractModelFromSessionFile(sessionID, cwd string) string {
	filePath := SessionFilePath(sessionID, cwd)
	if filePath == "" {
		return ""
	}

	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	// Only scan the first 50 lines to keep it fast.
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		if lineCount > 50 {
			break
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rec struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Type != "assistant" || len(rec.Message) == 0 {
			continue
		}

		var msg struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(rec.Message, &msg); err != nil || msg.Model == "" {
			continue
		}
		return classifyModel(msg.Model)
	}
	return ""
}
