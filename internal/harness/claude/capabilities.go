package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/stats"
)

var (
	_ harness.Resumer              = (*Harness)(nil)
	_ harness.Forker               = (*Harness)(nil)
	_ harness.HistoryProvider      = (*Harness)(nil)
	_ harness.ConversationProvider = (*Harness)(nil)
	_ harness.MetricsProvider      = (*Harness)(nil)
	_ harness.ChangedFilesProvider = (*Harness)(nil)
)

// ResumeCommand resumes by session ID; the literal binary is used (not the
// configured start command) because aliases may not accept --resume.
func (h *Harness) ResumeCommand(s *session.Session) string {
	return fmt.Sprintf("claude --resume %q", s.ID)
}

// ForkCommand resumes the session into a new forked session ID.
func (h *Harness) ForkCommand(s *session.Session) string {
	return h.ResumeCommand(s) + " --fork-session"
}

// ScanClosedAndAll implements harness.HistoryProvider.
func (h *Harness) ScanClosedAndAll(closedHours float64, activeIDs map[string]bool) (closed, all []*session.Session, err error) {
	if h.history == nil {
		return nil, nil, nil
	}
	return h.history.ScanClosedAndAll(closedHours, activeIDs)
}

// Summaries returns sessionID -> first prompt from ~/.claude/history.jsonl.
func (h *Harness) Summaries() (map[string]string, error) {
	if h.history == nil {
		return nil, nil
	}
	return h.history.LoadHistoryIndex()
}

// LoadConversation implements harness.ConversationProvider.
func (h *Harness) LoadConversation(s *session.Session, maxTurns int) ([]session.ConversationEntry, error) {
	return LoadConversation(s, maxTurns)
}

// LoadMetrics implements harness.MetricsProvider; nil metrics with no error
// means there is no transcript to read.
func (h *Harness) LoadMetrics(s *session.Session) (*session.SessionMetrics, error) {
	path := transcriptPath(s)
	if path == "" {
		return nil, nil
	}
	return LoadMetrics(path, s.Model)
}

// LoadChangedFiles implements harness.ChangedFilesProvider.
func (h *Harness) LoadChangedFiles(s *session.Session) ([]session.ChangedFile, error) {
	path := transcriptPath(s)
	if path == "" {
		return nil, nil
	}
	return LoadChangedFiles(path)
}

// ChangeStamp is the transcript mtime, which advances on every new edit.
func (h *Harness) ChangeStamp(s *session.Session) time.Time {
	path := transcriptPath(s)
	if path == "" {
		return time.Time{}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// transcriptPath is the session's .jsonl path, derived when not yet known.
func transcriptPath(s *session.Session) string {
	if s.SessionFile != "" {
		return s.SessionFile
	}
	return SessionFilePath(s.ID, s.CWD)
}

var _ harness.StatsProvider = (*Harness)(nil)

// StatsKey combines the transcript count with the newest transcript mtime so
// the stats cache invalidates when existing files grow, not just when new
// files appear.
func (h *Harness) StatsKey() int {
	fileCount := CountSessionFiles("")
	home, err := os.UserHomeDir()
	if err != nil {
		return fileCount
	}
	files, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	if err != nil {
		return fileCount
	}
	var maxMod int64
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			if mod := info.ModTime().Unix(); mod > maxMod {
				maxMod = mod
			}
		}
	}
	// The shift keeps the count from colliding with the mtime component.
	return fileCount + int(maxMod<<20)
}

// ComputeStats implements harness.StatsProvider.
func (h *Harness) ComputeStats(activeCount int, filter string) (*stats.Stats, error) {
	return Compute("", activeCount, filter)
}
