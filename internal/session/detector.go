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
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
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

// parentAppName checks the immediate parent of pid. If it's a non-shell
// program (e.g. "nvim"), returns its name -- meaning Claude is running as
// a subprocess. Returns "" if the parent is a shell or tmux (normal case).
func (t *ProcessTree) parentAppName(pid int) string {
	ppid, ok := t.ppid[pid]
	if !ok || ppid <= 1 {
		return ""
	}
	name := strings.TrimPrefix(strings.ToLower(t.names[ppid]), "-") // -zsh → zsh
	if isShellOrTmux(name) {
		return ""
	}
	return t.names[ppid]
}

func isShellOrTmux(name string) bool {
	switch name {
	case "zsh", "bash", "sh", "fish", "dash", "ksh", "csh", "tcsh", "nu", "login":
		return true
	}
	return strings.HasPrefix(name, "tmux")
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
	activeWindow time.Duration     // how recently .jsonl must be written to count as active
	cpuThreshold float64           // subtree CPU% above which a session counts as working
	tracker      *StatusTracker    // resolves final status with priority + hysteresis
	modelCacheMu sync.Mutex        // guards modelCache (Detect runs in overlapping tea.Cmd goroutines)
	modelCache   map[string]string // sessionID -> model (cached, never changes mid-session)

	lastActMu    sync.Mutex                   // guards lastActCache
	lastActCache map[string]lastActivityEntry // sessionID -> last-message time, keyed by file mtime

	aiTitleMu    sync.Mutex              // guards aiTitleCache
	aiTitleCache map[string]aiTitleEntry // sessionID -> ai-title, keyed by file mtime
}

// lastActivityEntry caches a session's last-message time alongside the file
// mtime it was computed from, so the transcript is only re-scanned when it
// actually changes.
type lastActivityEntry struct {
	mtime time.Time
	last  time.Time
}

// aiTitleEntry caches a session's ai-title alongside the transcript mtime it was
// read from, so the file is only re-scanned when it actually changes.
type aiTitleEntry struct {
	mtime time.Time
	title string
}

// NewDetector creates a Detector that uses the given tmux client and matches
// pane processes against processNames. If processNames is nil or empty, the
// DefaultProcessNames list is used. activeWindowSecs controls how many seconds
// after the last .jsonl write a session is considered active (0 = use default
// 5s); cpuThreshold is the subtree CPU% above which a session counts as working
// (<= 0 = use default 5).
func NewDetector(client *tmux.Client, processNames []string, activeWindowSecs int, cpuThreshold float64) *Detector {
	if len(processNames) == 0 {
		processNames = DefaultProcessNames
	}
	if activeWindowSecs <= 0 {
		activeWindowSecs = 5
	}
	if cpuThreshold <= 0 {
		cpuThreshold = 5.0
	}
	activeWindow := time.Duration(activeWindowSecs) * time.Second
	return &Detector{
		tmuxClient:   client,
		processNames: processNames,
		activeWindow: activeWindow,
		cpuThreshold: cpuThreshold,
		tracker:      NewStatusTracker(activeWindow),
		modelCache:   make(map[string]string),
		lastActCache: make(map[string]lastActivityEntry),
		aiTitleCache: make(map[string]aiTitleEntry),
	}
}

// cachedModel returns the model for a session, reading from cache or extracting
// from the .jsonl file on first access. A session's model never changes, so
// caching avoids repeated file I/O on every detect cycle.
func (d *Detector) cachedModel(sessionID, cwd string) string {
	// Bubble Tea runs each tea.Cmd in its own goroutine and multiple refresh
	// Cmds can overlap, so concurrent Detect goroutines may read/write the cache
	// at once. Hold the lock across the whole check-extract-store sequence; the
	// file read is fast and only happens on the first access per session.
	d.modelCacheMu.Lock()
	defer d.modelCacheMu.Unlock()
	if model, ok := d.modelCache[sessionID]; ok {
		return model
	}
	model := extractModelFromSessionFile(sessionID, cwd)
	if model != "" {
		d.modelCache[sessionID] = model
	}
	return model
}

// cachedLastActivity returns the timestamp of the most recent message in the
// session's transcript, scanning the file only when its mtime has changed since
// the last scan (the common case on the 200ms detect tick is a cache hit). It
// falls back to mtime when the transcript has no timestamped records (e.g. a
// brand-new session with no messages yet). Unlike mtime alone, this is not
// fooled by `claude --resume`, which bumps mtime to now without adding a newer
// message.
func (d *Detector) cachedLastActivity(sessionID, cwd string, mtime time.Time) time.Time {
	d.lastActMu.Lock()
	defer d.lastActMu.Unlock()
	if e, ok := d.lastActCache[sessionID]; ok && e.mtime.Equal(mtime) {
		return e.last
	}
	last := lastMessageTime(SessionFilePath(sessionID, cwd))
	if last.IsZero() {
		last = mtime // no timestamped records yet -- fall back to the write time
	}
	d.lastActCache[sessionID] = lastActivityEntry{mtime: mtime, last: last}
	return last
}

// cachedAITitle returns the session's latest ai-title, scanning the transcript
// only when its mtime has changed since the last read (the common case on the
// detect tick is a cache hit). Mirrors cachedLastActivity so the per-second
// detect tick does not re-read the whole transcript on every pass.
func (d *Detector) cachedAITitle(sessionID, cwd string, mtime time.Time) string {
	d.aiTitleMu.Lock()
	defer d.aiTitleMu.Unlock()
	if e, ok := d.aiTitleCache[sessionID]; ok && e.mtime.Equal(mtime) {
		return e.title
	}
	title := lastAITitle(SessionFilePath(sessionID, cwd))
	d.aiTitleCache[sessionID] = aiTitleEntry{mtime: mtime, title: title}
	return title
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
		s.Status = d.resolveStatus(s, tree)
		sessions = append(sessions, s)
	}

	// Drop hysteresis entries for tmux targets that are no longer live so the
	// tracker's map does not grow unbounded as panes are closed. Build the live
	// set from the targets detected this cycle (empty targets, e.g. subprocess
	// panes without a target, are never tracked by Resolve so need not be kept).
	activeTargets := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		if s.TmuxTarget != "" {
			activeTargets[s.TmuxTarget] = true
		}
	}
	d.tracker.Prune(activeTargets)

	return sessions, nil
}

// resolveStatus determines a session's status. Claude Code's own native status
// (from ~/.claude/sessions/<pid>.json) is authoritative when available: it
// already distinguishes working/waiting/idle and is debounced by the CLI, so no
// signal blending or hysteresis is applied. It is trusted regardless of how old
// its statusUpdatedAt timestamp is: Claude writes that timestamp only when the
// status CHANGES (event-driven, not a heartbeat), so a session sitting stably
// idle or busy legitimately carries an old timestamp. Demoting on age forces the
// unreliable content fallback, which false-positives WAITING on idle panes (the
// input box / leftover scrollback). Only when no native status exists (older
// Claude versions, or a missing file) does it fall back to combining the three
// legacy signals -- content classification, process subtree CPU, and transcript
// freshness -- via the tracker's priority (Waiting > Working > Idle) and
// hysteresis.
func (d *Detector) resolveStatus(s *Session, tree *ProcessTree) SessionStatus {
	now := time.Now()
	if status, ok := mapNativeStatus(s.nativeStatus); ok {
		return status
	}

	// Fallback: signal-based detection for Claude versions without a status field.
	// Content signal: skip subprocess panes (the pane belongs to the parent app,
	// not Claude, so its content must not be classified as a Claude prompt).
	signal := SignalNone
	if s.TmuxTarget != "" && !s.Subprocess {
		if content, err := d.tmuxClient.CapturePaneOutput(s.TmuxTarget); err == nil {
			signal = ClassifyPaneContent(content)
		}
	}

	cpuActive := tree.SubtreeCPU(s.PID) >= d.cpuThreshold

	transcriptActive := false
	if s.ID != "" {
		if mt := sessionFileModTime(s.ID, s.CWD); !mt.IsZero() {
			transcriptActive = now.Sub(mt) <= d.activeWindow
		}
	}

	return d.tracker.Resolve(s.TmuxTarget, signal, cpuActive, transcriptActive, now)
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

// stripANSIBasic removes ANSI/VT100 escape sequences from a string. It delegates
// to ansi.Strip (the same robust implementation used by internal/preview), which
// correctly handles CSI (ESC [ ...) and OSC (ESC ] ... BEL/ST) sequences that the
// previous hand-rolled scanner mishandled. Real Claude pane output only ever
// contains CSI and OSC sequences, so for those inputs this matches the preview
// path exactly.
//
// The one carve-out preserves a long-standing contract: a bare ESC that is NOT a
// CSI/OSC introducer (e.g. "a\x1bXb") is treated as a 2-byte drop (ESC + the next
// byte), matching the legacy scanner. ansi.Strip would instead treat ESC X as an
// SOS string-control opener and swallow everything up to its terminator, which is
// surprising for arbitrary mid-text bytes that are not really escape sequences.
func stripANSIBasic(s string) string {
	// Fast path: when every ESC introduces a CSI or OSC sequence (the only kinds
	// real Claude output contains), ansi.Strip handles the whole string. Only fall
	// back to the legacy byte scanner when a non-CSI/non-OSC ESC is present.
	if hasBareEscape(s) {
		return stripBareEscapeScanner(s)
	}
	return ansi.Strip(s)
}

// hasBareEscape reports whether s contains an ESC that is not immediately
// followed by '[' (CSI) or ']' (OSC) -- i.e. a sequence ansi.Strip would treat
// differently from the legacy 2-byte-drop behavior.
func hasBareEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			if i+1 >= len(s) {
				return true
			}
			if s[i+1] != '[' && s[i+1] != ']' {
				return true
			}
		}
	}
	return false
}

// stripBareEscapeScanner reproduces the legacy scanner: CSI sequences are
// consumed up to their final byte (0x40-0x7e), and any other ESC drops itself
// plus the following byte. Used only when hasBareEscape is true.
func stripBareEscapeScanner(s string) string {
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
	// First check whether the pane's own process is Claude. tmux's
	// pane_current_command is the fast signal, but newer Claude versions set
	// their process title to the version string (e.g. "2.1.191"), which tmux
	// reports instead of "claude" -- so also match the pane PID's real executable
	// basename from the ps-built tree. This matters when Claude is exec'd as the
	// pane's own process (e.g. a window started with `sh -c "cd x && claude"`,
	// where the shell execs claude as the final command), so it is the pane PID
	// itself rather than a descendant the walk below would find.
	if d.matchesProcessName(pane.CurrentCommand) || d.matchesProcessName(tree.names[pane.PID]) {
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

	// Read session metadata from ~/.claude/sessions/{pid}.json (the authoritative
	// source for running processes): session ID, Claude's own session name, and
	// live status. The per-PID file is the fast path.
	meta := readSessionMetadata(claudePID)
	if meta.SessionID != "" {
		s.ID = meta.SessionID
	} else {
		// No per-PID file -- typically because the PID naviClaude picked
		// (comm=="claude") is a launcher/wrapper, not the PID Claude wrote its
		// <pid>.json under. Resolve the sessionId from other signals, then recover
		// the name from the file whose sessionId matches, so it stays as reliable
		// as the sessionId itself. (Previously only status had this fallback; the
		// name could go stale on a PID mismatch, and a /clear or /new name change
		// could be missed.) Status is resolved below via the shared helper.
		s.ID = extractSessionIDFallback(claudePID, pane.CurrentPath)
		if byID, ok := readSessionMetadataByID(s.ID); ok {
			meta.Name = byID.Name
			meta.NameSource = byID.NameSource
		}
	}
	// Claude Code's own status (busy|waiting|idle|shell) plus its timestamp,
	// authoritative for resolveStatus when present and fresh. Resolve it the same
	// way the preview path (nativeStatusString) does -- prefer the per-PID file
	// (already read into meta above), but fall back to the by-sessionId scan
	// WHENEVER the per-PID status is empty (not only when the per-PID file is
	// missing), so both code paths agree on a PID/launcher mismatch. s.ID is set
	// above, so the fallback scan can find the right file. Empty status means an
	// older Claude with no status field, in which case resolveStatus uses
	// signal-based detection.
	statusMeta := meta
	if statusMeta.Status == "" {
		if byID, ok := readSessionMetadataByID(s.ID); ok {
			statusMeta = byID
		}
	}
	s.nativeStatus = statusMeta.Status
	s.nativeStatusAt = statusMeta.StatusUpdatedAt

	// Detect if Claude is running as a subprocess (e.g. inside neovim).
	if parentApp := tree.parentAppName(claudePID); parentApp != "" {
		s.Subprocess = true
		s.SubprocessParent = parentApp
	}

	// Populate CPU and memory from the bulk process tree (no per-session ps call).
	s.CPU, s.Memory = tree.Stats(claudePID)

	// Record the model and the transcript's last-message time, used for the
	// UI's relative-time display, sorting, and auto-collapse. This does NOT feed
	// status detection: Detect computes the "recently written" signal from a
	// fresh mtime stat (see resolveStatus), so display time and status stay
	// independent.
	// Resolve the display title from the per-PID name and the transcript's
	// ai-title (see resolveTitle). aiTitle stays "" until a transcript exists on
	// disk, in which case resolveTitle falls back to the per-PID name.
	aiTitle := ""
	if s.ID != "" {
		s.Model = d.cachedModel(s.ID, pane.CurrentPath)
		if modTime := sessionFileModTime(s.ID, pane.CurrentPath); !modTime.IsZero() {
			// Use the last message timestamp (not the file mtime) so a resumed
			// session shows when its conversation last happened, not "now".
			s.LastActivity = d.cachedLastActivity(s.ID, pane.CurrentPath, modTime)
			aiTitle = d.cachedAITitle(s.ID, pane.CurrentPath, modTime)
		}
	}
	s.DisplayName = resolveTitle(meta.Name, meta.NameSource, aiTitle)

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
func mapNativeStatus(status string) (SessionStatus, bool) {
	switch status {
	case "busy":
		return StatusActive, true
	case "waiting":
		return StatusWaiting, true
	case "idle", "shell":
		return StatusIdle, true
	default:
		return StatusIdle, false
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

// NativeStatus reads Claude Code's own status for a session and maps it to a
// SessionStatus. It tries the per-PID file first, then a sessionId scan. The
// bool is false when no usable native status is available (file missing, or a
// Claude version that does not write the status field), in which case callers
// should fall back to signal-based detection. This is the single source of truth
// shared by the detector loop and the preview path.
func NativeStatus(pid int, sessionID string) (SessionStatus, bool) {
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
