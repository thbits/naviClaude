// Package harness defines the boundary between naviClaude's generic tmux
// dashboard and the agent CLIs ("harnesses") whose sessions it observes.
package harness

import (
	"time"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/stats"
)

// Info holds static facts about a harness used by generic code and the UI.
type Info struct {
	Name           string        // stable key stamped on sessions, e.g. "claude"
	Label          string        // human-facing name, e.g. "Claude"
	AssistantLabel string        // conversation role label for the agent's turns
	PromptCacheTTL time.Duration // idle time after which the prompt cache expires; 0 = none
}

// Harness is the required surface for recognizing live sessions and starting
// new ones. Implementations are called from concurrent goroutines and must be
// goroutine-safe.
type Harness interface {
	Info() Info
	// MatchProcess reports whether a process basename or tmux pane command
	// belongs to this harness.
	MatchProcess(name string) bool
	// Enrich fills harness-sourced fields (ID, title, model, LastActivity,
	// SessionFile) on a session whose tmux/process fields are already set.
	Enrich(s *session.Session)
	// NativeStatus returns the harness's own authoritative status, if any.
	NativeStatus(s *session.Session) (session.SessionStatus, bool)
	// ClassifyPane extracts a status signal from captured pane text.
	ClassifyPane(content string) session.PaneSignal
	// RecentlyActive reports whether the session wrote activity within window.
	RecentlyActive(s *session.Session, window time.Duration) bool
	// StartCommand is the shell command that starts a new session.
	StartCommand() string
}

// Resumer can resume a closed session.
type Resumer interface {
	ResumeCommand(s *session.Session) string
}

// Forker can resume a session as a fork.
type Forker interface {
	ForkCommand(s *session.Session) string
}

// HistoryProvider lists sessions that are no longer running. activeIDs and the
// keys of Summaries are this harness's own bare session IDs.
type HistoryProvider interface {
	ScanClosedAndAll(closedHours float64, activeIDs map[string]bool) (closed, all []*session.Session, err error)
	Summaries() (map[string]string, error)
}

// ConversationProvider loads a session's conversation for the closed preview.
type ConversationProvider interface {
	LoadConversation(s *session.Session, maxTurns int) ([]session.ConversationEntry, error)
}

// MetricsProvider loads token/turn metrics for a session.
type MetricsProvider interface {
	LoadMetrics(s *session.Session) (*session.SessionMetrics, error)
}

// ChangedFilesProvider lists files the agent edited. ChangeStamp moves forward
// whenever the list may have changed.
type ChangedFilesProvider interface {
	LoadChangedFiles(s *session.Session) ([]session.ChangedFile, error)
	ChangeStamp(s *session.Session) time.Time
}

// StatsProvider computes aggregate usage statistics. StatsKey changes whenever
// a cached result would be stale.
type StatsProvider interface {
	StatsKey() int
	ComputeStats(activeCount int, filter string) (*stats.Stats, error)
}

// AsResumer returns h as a Resumer when it supports resuming.
func AsResumer(h Harness) (Resumer, bool) { r, ok := h.(Resumer); return r, ok }

// AsForker returns h as a Forker when it supports fork-resume.
func AsForker(h Harness) (Forker, bool) { f, ok := h.(Forker); return f, ok }

// AsHistory returns h as a HistoryProvider when it exposes closed sessions.
func AsHistory(h Harness) (HistoryProvider, bool) { p, ok := h.(HistoryProvider); return p, ok }

// AsConversation returns h as a ConversationProvider when supported.
func AsConversation(h Harness) (ConversationProvider, bool) {
	p, ok := h.(ConversationProvider)
	return p, ok
}

// AsMetrics returns h as a MetricsProvider when supported.
func AsMetrics(h Harness) (MetricsProvider, bool) { p, ok := h.(MetricsProvider); return p, ok }

// AsChangedFiles returns h as a ChangedFilesProvider when supported.
func AsChangedFiles(h Harness) (ChangedFilesProvider, bool) {
	p, ok := h.(ChangedFilesProvider)
	return p, ok
}

// AsStats returns h as a StatsProvider when supported.
func AsStats(h Harness) (StatsProvider, bool) { p, ok := h.(StatsProvider); return p, ok }
