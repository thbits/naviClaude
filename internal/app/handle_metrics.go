package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
)

// metricsMsg carries loaded metrics for the selected session.
type metricsMsg struct {
	sessionID string
	metrics   *session.SessionMetrics
}

// loadMetricsCmd loads metrics from a session's JSONL file asynchronously.
func loadMetricsCmd(sess *session.Session) tea.Cmd {
	return func() tea.Msg {
		filePath := sess.SessionFile
		if filePath == "" && sess.ID != "" && sess.CWD != "" {
			filePath = session.SessionFilePath(sess.ID, sess.CWD)
		}
		if filePath == "" {
			return metricsMsg{sessionID: sess.ID}
		}
		// A scan error (e.g. a transcript line over the scanner buffer) still
		// yields useful partial metrics, so only a nil result -- an outright
		// open failure -- is treated as "no data".
		m, _ := session.LoadMetrics(filePath, sess.Model)
		if m == nil {
			return metricsMsg{sessionID: sess.ID}
		}
		return metricsMsg{sessionID: sess.ID, metrics: m}
	}
}
