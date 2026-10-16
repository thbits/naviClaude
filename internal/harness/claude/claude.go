// Package claude implements the harness for Claude Code sessions, reading the
// CLI's on-disk state under ~/.claude.
package claude

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
)

// Name is the harness key stamped on Claude sessions.
const Name = "claude"

// DefaultProcessNames is the default list of process names that indicate a
// Claude session is running in a tmux pane.
var DefaultProcessNames = []string{"claude"}

// Options configures the Claude harness.
type Options struct {
	Command      string   // shell command for new sessions; "" = "claude"
	ProcessNames []string // process basenames to match; empty = DefaultProcessNames
}

// Harness is the Claude Code harness.
type Harness struct {
	command      string
	processNames []string
	history      *HistoryScanner

	modelCacheMu sync.Mutex        // Enrich runs in overlapping tea.Cmd goroutines
	modelCache   map[string]string // sessionID -> model (never changes mid-session)

	lastActMu    sync.Mutex
	lastActCache map[string]lastActivityEntry // sessionID -> last-message time, keyed by file mtime

	aiTitleMu    sync.Mutex
	aiTitleCache map[string]aiTitleEntry // sessionID -> ai-title, keyed by file mtime
}

// lastActivityEntry caches a session's last-message time alongside the file
// mtime it was computed from, so the transcript is only re-scanned on change.
type lastActivityEntry struct {
	mtime time.Time
	last  time.Time
}

// aiTitleEntry caches a session's ai-title alongside the transcript mtime.
type aiTitleEntry struct {
	mtime time.Time
	title string
}

var _ harness.Harness = (*Harness)(nil)

// New creates a Claude harness.
func New(o Options) *Harness {
	if o.Command == "" {
		o.Command = "claude"
	}
	if len(o.ProcessNames) == 0 {
		o.ProcessNames = DefaultProcessNames
	}
	hs, _ := NewHistoryScanner("")
	return &Harness{
		command:      o.Command,
		processNames: o.ProcessNames,
		history:      hs,
		modelCache:   make(map[string]string),
		lastActCache: make(map[string]lastActivityEntry),
		aiTitleCache: make(map[string]aiTitleEntry),
	}
}

// Info implements harness.Harness.
func (h *Harness) Info() harness.Info {
	return harness.Info{
		Name:           Name,
		Label:          "Claude",
		AssistantLabel: "Claude",
		PromptCacheTTL: time.Hour,
	}
}

// MatchProcess reports whether name matches a configured process name,
// case-insensitively and by basename to handle absolute paths.
func (h *Harness) MatchProcess(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	for _, want := range h.processNames {
		if base == strings.ToLower(want) {
			return true
		}
	}
	return false
}

// StartCommand implements harness.Harness.
func (h *Harness) StartCommand() string { return h.command }

// Enrich resolves the session ID, title, model, last activity and transcript
// path from ~/.claude for a live session.
func (h *Harness) Enrich(s *session.Session) {
	// The per-PID file is the fast, authoritative path for running processes.
	meta := readSessionMetadata(s.PID)
	if meta.SessionID != "" {
		s.ID = meta.SessionID
	} else {
		// No per-PID file usually means the matched PID is a launcher/wrapper;
		// resolve the ID elsewhere, then recover the name by sessionId.
		s.ID = extractSessionIDFallback(s.PID, s.CWD)
		if byID, ok := readSessionMetadataByID(s.ID); ok {
			meta.Name = byID.Name
			meta.NameSource = byID.NameSource
		}
	}

	// aiTitle stays "" until a transcript exists, so resolveTitle falls back to
	// the per-PID name.
	aiTitle := ""
	if s.ID != "" {
		s.SessionFile = SessionFilePath(s.ID, s.CWD)
		s.Model = h.cachedModel(s.ID, s.CWD)
		if modTime := sessionFileModTime(s.ID, s.CWD); !modTime.IsZero() {
			// Last message time (not mtime) so a resumed session shows when its
			// conversation last happened, not "now".
			s.LastActivity = h.cachedLastActivity(s.ID, s.CWD, modTime)
			aiTitle = h.cachedAITitle(s.ID, s.CWD, modTime)
		}
	}
	s.DisplayName = resolveTitle(meta.Name, meta.NameSource, aiTitle)
}

// NativeStatus returns Claude Code's own status from ~/.claude/sessions,
// trusted regardless of statusUpdatedAt age (it is event-driven, not a heartbeat).
// It also records why the session is waiting on s.
func (h *Harness) NativeStatus(s *session.Session) (session.SessionStatus, bool) {
	st, ok := ReadNativeState(s.PID, s.ID)
	if !ok {
		return session.StatusIdle, false
	}
	status, ok := mapNativeStatus(st.Status)
	s.NativeStatus = ok
	s.WaitingFor = ""
	if status == session.StatusWaiting {
		s.WaitingFor = st.WaitingFor
	}
	return status, ok
}

// ClassifyPane implements harness.Harness.
func (h *Harness) ClassifyPane(content string) session.PaneSignal {
	return ClassifyPaneContent(content)
}

// RecentlyActive reports whether the transcript was written within window.
func (h *Harness) RecentlyActive(s *session.Session, window time.Duration) bool {
	if s.ID == "" {
		return false
	}
	mt := sessionFileModTime(s.ID, s.CWD)
	return !mt.IsZero() && time.Since(mt) <= window
}

// cachedModel returns the session's model, reading the transcript only on
// first access. The lock spans check-extract-store so concurrent Enrich calls
// don't duplicate the read.
func (h *Harness) cachedModel(sessionID, cwd string) string {
	h.modelCacheMu.Lock()
	defer h.modelCacheMu.Unlock()
	if model, ok := h.modelCache[sessionID]; ok {
		return model
	}
	model := extractModelFromSessionFile(sessionID, cwd)
	if model != "" {
		h.modelCache[sessionID] = model
	}
	return model
}

// cachedLastActivity returns the transcript's last-message time, rescanning only
// when mtime changes and falling back to mtime when no record has a timestamp.
// Unlike mtime alone this is not fooled by `claude --resume` touching the file.
func (h *Harness) cachedLastActivity(sessionID, cwd string, mtime time.Time) time.Time {
	h.lastActMu.Lock()
	defer h.lastActMu.Unlock()
	if e, ok := h.lastActCache[sessionID]; ok && e.mtime.Equal(mtime) {
		return e.last
	}
	last := lastMessageTime(SessionFilePath(sessionID, cwd))
	if last.IsZero() {
		last = mtime
	}
	h.lastActCache[sessionID] = lastActivityEntry{mtime: mtime, last: last}
	return last
}

// cachedAITitle returns the latest ai-title, rescanning only when mtime changes.
func (h *Harness) cachedAITitle(sessionID, cwd string, mtime time.Time) string {
	h.aiTitleMu.Lock()
	defer h.aiTitleMu.Unlock()
	if e, ok := h.aiTitleCache[sessionID]; ok && e.mtime.Equal(mtime) {
		return e.title
	}
	title := lastAITitle(SessionFilePath(sessionID, cwd))
	h.aiTitleCache[sessionID] = aiTitleEntry{mtime: mtime, title: title}
	return title
}
