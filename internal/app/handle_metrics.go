package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
)

// metricsMsg carries loaded metrics for the selected session.
type metricsMsg struct {
	sessionID string
	metrics   *session.SessionMetrics
}

// loadMetricsCmd loads a session's metrics asynchronously via its harness.
func loadMetricsCmd(h harness.Harness, sess *session.Session) tea.Cmd {
	return func() tea.Msg {
		mp, ok := harness.AsMetrics(h)
		if h == nil || !ok {
			return metricsMsg{sessionID: sess.ID}
		}
		// A read error mid-file still yields useful partial metrics; only a nil
		// result (an open failure) means "no data".
		m, _ := mp.LoadMetrics(sess)
		if m == nil {
			return metricsMsg{sessionID: sess.ID}
		}
		return metricsMsg{sessionID: sess.ID, metrics: m}
	}
}
