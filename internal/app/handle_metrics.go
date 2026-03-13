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
		m, err := session.LoadMetrics(filePath, sess.Model)
		if err != nil {
			return metricsMsg{sessionID: sess.ID}
		}
		return metricsMsg{sessionID: sess.ID, metrics: m}
	}
}
