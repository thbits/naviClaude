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
	"sync"
	"syscall"
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

// extractSessionIDFallback resolves the session ID when pid has no
// ~/.claude/sessions/<pid>.json of its own.
func extractSessionIDFallback(pid int, cwd, paneID string) string {
	live := liveSessionMetadata()
	// Claude records its tmux pane, which finds the file when the matched PID is a launcher.
	if paneID != "" {
		for _, m := range live {
			if m.SessionID != "" && tmuxPaneID(m.Tmux) == paneID {
				return m.SessionID
			}
		}
	}

	proc := lookupProc(pid)
	if id := parseResumeFlag(proc.cmdLine); id != "" {
		return id
	}

	// Guess from transcripts, skipping ones another live pane owns and ones that
	// predate this process: a just-started session has no transcript of its own yet.
	claimed := make(map[string]bool)
	for _, m := range live {
		if p := tmuxPaneID(m.Tmux); p != "" && p != paneID {
			claimed[m.SessionID] = true
		}
	}
	return findLatestSessionFile(cwd, proc.start, claimed)
}

// liveSessionMetadata returns the metadata files of Claude processes still running.
func liveSessionMetadata() []sessionMetadata {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var live []sessionMetadata
	for _, e := range entries {
		stem, ok := strings.CutSuffix(e.Name(), ".json")
		if e.IsDir() || !ok {
			continue
		}
		pid, err := strconv.Atoi(stem)
		if err != nil || !processAlive(pid) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		live = append(live, parseSessionMetadata(data))
	}
	return live
}

// processAlive reports whether pid exists; EPERM still means it is running.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// tmuxPaneID extracts the "%N" pane id from Claude's "session:@win.%pane" tmux field.
func tmuxPaneID(tmux string) string {
	i := strings.LastIndex(tmux, ".%")
	if i < 0 {
		return ""
	}
	return tmux[i+1:]
}

// procInfo is what ps reports about a process; neither field changes while it runs.
type procInfo struct {
	start   time.Time // to the second
	cmdLine string
	fetched time.Time
}

// procCache memoizes procInfo by PID so each refresh tick doesn't fork ps; entries
// expire after procCacheTTL so a reused PID is soon re-read.
var procCache sync.Map

const procCacheTTL = time.Minute

// lookupProc returns pid's start time and command line, zero if it is not running.
func lookupProc(pid int) procInfo {
	if v, ok := procCache.Load(pid); ok && time.Since(v.(procInfo).fetched) < procCacheTTL {
		return v.(procInfo)
	}
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=", "-o", "command=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return procInfo{}
	}
	f := strings.Fields(string(out))
	if len(f) < 5 {
		return procInfo{}
	}
	start, err := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(f[:5], " "), time.Local)
	if err != nil {
		return procInfo{}
	}
	p := procInfo{start: start, cmdLine: strings.Join(f[5:], " "), fetched: time.Now()}
	procCache.Store(pid, p)
	return p
}

// sessionMetadata holds fields from ~/.claude/sessions/{pid}.json. Claude Code
// writes this file per running process and keeps Status live (Claude >= 2.1).
type sessionMetadata struct {
	SessionID       string `json:"sessionId"`
	Tmux            string `json:"tmux"` // "session:@window.%pane" Claude runs in
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

// parseSessionMetadata unmarshals session-metadata JSON; malformed input yields the zero struct.
func parseSessionMetadata(data []byte) sessionMetadata {
	var meta sessionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return sessionMetadata{}
	}
	return meta
}

// mapNativeStatus maps Claude's status field to a SessionStatus; ok is false for an
// empty or unknown value (older Claude), so callers fall back to signal detection.
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

// readSessionMetadataByID finds a live session's metadata by sessionId, for when
// the matched PID is a launcher that owns no <pid>.json.
func readSessionMetadataByID(sessionID string) (sessionMetadata, bool) {
	if sessionID == "" {
		return sessionMetadata{}, false
	}
	for _, m := range liveSessionMetadata() {
		if m.SessionID == sessionID {
			return m, true
		}
	}
	return sessionMetadata{}, false
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

// parseResumeFlag extracts the ID from --resume/-r, inline or space-separated. A
// following "-flag" is not an ID: bare --resume opens Claude's session picker.
func parseResumeFlag(cmdLine string) string {
	parts := strings.Fields(cmdLine)
	for i, p := range parts {
		switch {
		case strings.HasPrefix(p, "--resume="):
			return strings.TrimPrefix(p, "--resume=")
		case strings.HasPrefix(p, "-r="):
			return strings.TrimPrefix(p, "-r=")
		case p == "--resume" || p == "-r":
			if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "-") {
				return parts[i+1]
			}
		}
	}
	return ""
}

// findLatestSessionFile returns the ID of cwd's newest transcript, ignoring ones
// modified before notBefore or listed in skip.
func findLatestSessionFile(cwd string, notBefore time.Time, skip map[string]bool) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

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
		if skip[strings.TrimSuffix(e.Name(), ".jsonl")] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(notBefore) {
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

// extractModelFromSessionFile reads the model from the transcript's first
// assistant messages, so live sessions show it before they close.
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
