// Package claude implements the harness for Claude Code sessions, reading the
// CLI's on-disk state under ~/.claude.
package claude

import (
	"os"
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

	metaMu    sync.Mutex
	metaCache map[metaKey]*transcriptMeta // transcript state, rescanned incrementally on change
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
		metaCache:    make(map[metaKey]*transcriptMeta),
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
		s.ID = extractSessionIDFallback(s.PID, s.CWD, s.TmuxPaneID)
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
		if info, err := os.Stat(s.SessionFile); err == nil {
			// Last message time (not mtime) so a resumed session shows when its
			// conversation last happened, not "now".
			s.LastActivity, aiTitle = h.cachedTranscriptMeta(s.ID, s.SessionFile, info)
		}
	}
	s.DisplayName = resolveTitle(meta.Name, meta.NameSource, aiTitle)
}

// NativeStatus returns Claude's own status from ~/.claude/sessions, trusted at any
// age since it is event-driven, and records NativeStatus and WaitingFor on s.
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

// cachedModel returns the session's model, read once; the lock spans
// check-read-store so concurrent Enrich calls don't duplicate the read.
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

// cachedTranscriptMeta returns the last-message time (else mtime; unlike mtime it
// isn't bumped by `claude --resume`) and the ai-title, reading only appended bytes.
func (h *Harness) cachedTranscriptMeta(sessionID, path string, info os.FileInfo) (time.Time, string) {
	key := metaKey{id: sessionID, path: path}
	// Held across check-scan-store so overlapping Enrich calls don't scan twice.
	h.metaMu.Lock()
	defer h.metaMu.Unlock()
	m := h.metaCache[key]
	if m == nil || m.size != info.Size() || !m.mtime.Equal(info.ModTime()) {
		if next, err := scanTranscriptMeta(path, m); err == nil {
			m = next
			h.metaCache[key] = m
		}
	}
	if m == nil {
		return info.ModTime(), ""
	}
	if m.lastTS.IsZero() {
		return info.ModTime(), m.aiTitle
	}
	return m.lastTS, m.aiTitle
}
