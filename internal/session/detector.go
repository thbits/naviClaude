package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thbits/naviClaude/internal/tmux"
)

// DefaultProcessNames is the default list of process names that indicate a
// Claude session is running in a tmux pane.
var DefaultProcessNames = []string{"claude"}

// ProcessTree holds the entire process tree from a single ps call, including
// CPU and RSS stats to avoid per-session ps calls.
type ProcessTree struct {
	children map[int][]int   // parent -> child PIDs
	names    map[int]string  // PID -> process name (basename)
	ppid     map[int]int     // PID -> parent PID
	cpu      map[int]float64 // PID -> CPU %
	rss      map[int]float64 // PID -> RSS in KB
}

// BuildProcessTree runs a single `ps -eo pid=,ppid=,%cpu=,rss=,comm=` command
// and builds an in-memory process tree with stats. This replaces hundreds of
// per-PID pgrep/ps calls.
func BuildProcessTree() *ProcessTree {
	out, err := exec.Command("ps", "-eo", "pid=,ppid=,%cpu=,rss=,comm=").Output()
	if err != nil {
		return &ProcessTree{
			children: make(map[int][]int),
			names:    make(map[int]string),
			ppid:     make(map[int]int),
			cpu:      make(map[int]float64),
			rss:      make(map[int]float64),
		}
	}

	tree := &ProcessTree{
		children: make(map[int][]int),
		names:    make(map[int]string),
		ppid:     make(map[int]int),
		cpu:      make(map[int]float64),
		rss:      make(map[int]float64),
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Fields: PID PPID %CPU RSS COMM (comm may contain spaces for full path)
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		cpuVal, _ := strconv.ParseFloat(fields[2], 64)
		rssVal, _ := strconv.ParseFloat(fields[3], 64)
		// comm is the rest of the fields joined (may be a path)
		comm := strings.Join(fields[4:], " ")
		name := filepath.Base(comm)

		tree.names[pid] = name
		tree.ppid[pid] = ppid
		tree.children[ppid] = append(tree.children[ppid], pid)
		tree.cpu[pid] = cpuVal
		tree.rss[pid] = rssVal
	}

	return tree
}

// Stats returns CPU % and memory MB for a PID from the pre-built tree.
func (t *ProcessTree) Stats(pid int) (cpu float64, memMB float64) {
	return t.cpu[pid], t.rss[pid] / 1024.0
}


// isAncestorOf reports whether ancestorPID is an ancestor of descendantPID
// using the pre-built process tree.
func (t *ProcessTree) isAncestorOf(ancestorPID, descendantPID int) bool {
	pid := descendantPID
	for i := 0; i < 10; i++ {
		ppid, ok := t.ppid[pid]
		if !ok || ppid <= 1 {
			return false
		}
		if ppid == ancestorPID {
			return true
		}
		pid = ppid
	}
	return false
}

// findMatchingDescendant performs a depth-first walk of the process tree from
// rootPID and returns the PID of the first descendant whose process name
// matches any of the given names. Returns 0 if no match is found.
func (t *ProcessTree) findMatchingDescendant(rootPID int, matchFunc func(string) bool, maxDepth int) int {
	if maxDepth == 0 {
		return 0
	}
	for _, child := range t.children[rootPID] {
		name := t.names[child]
		if matchFunc(name) {
			return child
		}
		if found := t.findMatchingDescendant(child, matchFunc, maxDepth-1); found != 0 {
			return found
		}
	}
	return 0
}

// Detector discovers active Claude sessions from tmux panes by inspecting the
// process tree of each pane.
type Detector struct {
	tmuxClient   *tmux.Client
	processNames []string
	activeWindow time.Duration    // how recently .jsonl must be written to count as active
	modelCache   map[string]string // sessionID -> model (cached, never changes mid-session)
}

// NewDetector creates a Detector that uses the given tmux client and matches
// pane processes against processNames. If processNames is nil or empty, the
// DefaultProcessNames list is used. activeWindowSecs controls how many seconds
// after the last .jsonl write a session is considered active (0 = use default 5s).
func NewDetector(client *tmux.Client, processNames []string, activeWindowSecs int) *Detector {
	if len(processNames) == 0 {
		processNames = DefaultProcessNames
	}
	if activeWindowSecs <= 0 {
		activeWindowSecs = 5
	}
	return &Detector{
		tmuxClient:   client,
		processNames: processNames,
		activeWindow: time.Duration(activeWindowSecs) * time.Second,
		modelCache:   make(map[string]string),
	}
}

// cachedModel returns the model for a session, reading from cache or extracting
// from the .jsonl file on first access. A session's model never changes, so
// caching avoids repeated file I/O on every detect cycle.
func (d *Detector) cachedModel(sessionID, cwd string) string {
	if model, ok := d.modelCache[sessionID]; ok {
		return model
	}
	model := extractModelFromSessionFile(sessionID, cwd)
	if model != "" {
		d.modelCache[sessionID] = model
	}
	return model
}

// Detect returns all active sessions found by walking the process tree of
// every tmux pane. It uses a single bulk ps call for the entire process tree.
// It also performs lightweight prompt detection on each session's pane to
// determine waiting-for-input status.
func (d *Detector) Detect() ([]*Session, error) {
	panes, err := d.tmuxClient.ListPanes()
	if err != nil {
		return nil, fmt.Errorf("detector: list panes: %w", err)
	}

	// Build the process tree once for all panes.
	tree := BuildProcessTree()

	// Get our own PID to filter out the pane naviClaude is running in.
	selfPID := os.Getpid()

	var sessions []*Session
	for _, pane := range panes {
		// Skip popup panes (tmux 3.3+ uses "[popup]" as session name).
		if strings.HasPrefix(pane.SessionName, "[popup]") || pane.SessionName == "popup" {
			continue
		}
		// Skip our own pane (don't detect naviClaude itself).
		if pane.PID == selfPID || tree.isAncestorOf(pane.PID, selfPID) {
			continue
		}
		s := d.sessionFromPane(pane, tree)
		if s == nil {
			continue
		}
		// Prompt detection runs regardless of active/idle status -- a waiting
		// prompt always takes priority.
		if content, err := d.tmuxClient.CapturePaneOutput(s.TmuxTarget); err == nil {
			if isWaitingForInput(content) {
				s.Status = StatusWaiting
			}
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// isWaitingForInput checks if the pane content shows a Claude Code interactive
// prompt. We do NOT look for the ❯ (U+276F) character because it is always
// visible in every Claude session as part of the standard TUI layout (the input
// prompt sits between separator lines and is always rendered, even when Claude
// just finished responding).
//
// Instead we detect specific interactive UI patterns:
//   - Confirmation prompts: [Y/n], [y/N], Allow?
//   - Interactive selections: "Enter to select", "Enter to confirm"
//   - Interrupted sessions: "What should Claude do instead?"
//   - Permission prompts with action hints
func isWaitingForInput(content string) bool {
	lines := lastNNonEmptyLines(content, 6)
	for _, line := range lines {
		lower := strings.ToLower(line)

		// Confirmation prompts: [Y/n] or [y/N]
		if strings.Contains(lower, "[y/n]") || strings.Contains(lower, "[n/y]") {
			return true
		}

		// Permission request
		if strings.Contains(line, "Allow?") {
			return true
		}

		// Interactive selection menus (onboarding, trust dialog, etc.)
		if strings.Contains(lower, "enter to select") ||
			strings.Contains(lower, "enter to confirm") {
			return true
		}

		// Interrupted session waiting for new direction
		if strings.Contains(line, "What should Claude do instead?") {
			return true
		}
	}
	return false
}

// lastNNonEmptyLines returns up to n non-empty lines from the end of the string
// after stripping ANSI escape sequences.
func lastNNonEmptyLines(s string, n int) []string {
	lines := strings.Split(s, "\n")
	var result []string
	for i := len(lines) - 1; i >= 0 && len(result) < n; i-- {
		stripped := stripANSIBasic(lines[i])
		if strings.TrimSpace(stripped) != "" {
			result = append(result, stripped)
		}
	}
	return result
}

// stripANSIBasic removes ANSI/VT100 escape sequences and common Unicode
// control characters from a string.
func stripANSIBasic(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) {
				if s[i] >= 0x40 && s[i] <= 0x7e {
					i++
					break
				}
				i++
			}
			continue
		}
		if c == '\x1b' {
			i += 2
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// sessionFromPane checks whether a pane is running a Claude process (directly
// or as a descendant), and if so returns a Session for it.
func (d *Detector) sessionFromPane(pane tmux.PaneInfo, tree *ProcessTree) *Session {
	// First check the pane's direct current command.
	if d.matchesProcessName(pane.CurrentCommand) {
		return d.buildSession(pane, pane.PID, tree)
	}

	// Walk the process tree from the pane's PID to find a matching descendant.
	claudePID := tree.findMatchingDescendant(pane.PID, d.matchesProcessName, 6)
	if claudePID == 0 {
		return nil
	}
	return d.buildSession(pane, claudePID, tree)
}

// buildSession constructs a Session from PaneInfo. The claudePID is the PID
// of the actual Claude process (which may be a grandchild of the pane's shell).
func (d *Detector) buildSession(pane tmux.PaneInfo, claudePID int, tree *ProcessTree) *Session {
	s := &Session{
		TmuxSession:  pane.SessionName,
		TmuxTarget:   pane.Target,
		CWD:          pane.CurrentPath,
		Status:       StatusIdle, // default; promoted below
		LastActivity: time.Now(),
		ProjectName:  filepath.Base(pane.CurrentPath),
		PID:          claudePID,
	}

	// Try to extract session ID from the Claude process command line.
	s.ID = extractSessionID(claudePID, pane.CurrentPath)

	// Populate CPU and memory from the bulk process tree (no per-session ps call).
	s.CPU, s.Memory = tree.Stats(claudePID)

	// Determine status from .jsonl mod time:
	//   Active  -- the .jsonl file was written within the last 5s,
	//              meaning Claude was recently generating or running tools
	//   Idle    -- process running but the file hasn't been written recently
	if s.ID != "" {
		s.Model = d.cachedModel(s.ID, pane.CurrentPath)
		if modTime := sessionFileModTime(s.ID, pane.CurrentPath); !modTime.IsZero() {
			s.LastActivity = modTime
			if time.Since(modTime) <= d.activeWindow {
				s.Status = StatusActive
			}
		}
	}

	return s
}

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

// extractSessionID attempts to determine the session ID for a running Claude
// process. It first checks the command line for --resume <sessionId>, then
// falls back to finding the most recently modified .jsonl file in the matching
// project directory.
func extractSessionID(pid int, cwd string) string {
	// Try command line first: "claude --resume <uuid>"
	cmdLine := fullCommandLine(pid)
	if id := parseResumeFlag(cmdLine); id != "" {
		return id
	}

	// Fall back to most recently modified .jsonl in the project dir.
	return findLatestSessionFile(cwd)
}

// fullCommandLine returns the full command line of a process.
func fullCommandLine(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseResumeFlag extracts the session ID from a --resume flag in a command line.
func parseResumeFlag(cmdLine string) string {
	parts := strings.Fields(cmdLine)
	for i, p := range parts {
		if p == "--resume" && i+1 < len(parts) {
			return parts[i+1]
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
	slug := strings.ReplaceAll(cwd, "/", "-")
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

// matchesProcessName reports whether name (typically from pane_current_command)
// matches any entry in the detector's processNames list. The comparison is
// case-insensitive and also checks the basename to handle absolute paths.
func (d *Detector) matchesProcessName(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	for _, want := range d.processNames {
		if base == strings.ToLower(want) {
			return true
		}
	}
	return false
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
