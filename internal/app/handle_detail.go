package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
)

// detailDataMsg carries data loaded from the session's .jsonl file. It carries
// the session ID so a slow read doesn't repaint the detail popup after the user
// has reopened it on a different session (mirrors metricsMsg).
type detailDataMsg struct {
	sessionID    string
	messageCount int
	startTime    time.Time
}

// loadDetailDataCmd counts conversational turns and finds the start time via
// the harness's metrics, so the detail popup and the preview header agree.
func loadDetailDataCmd(h harness.Harness, sess *session.Session) tea.Cmd {
	sessionID := sess.ID
	return func() tea.Msg {
		mp, ok := harness.AsMetrics(h)
		if h == nil || !ok {
			return detailDataMsg{sessionID: sessionID}
		}

		// A scan error still yields useful partial counts, so only a nil result
		// (open failure) is treated as "no data".
		m, _ := mp.LoadMetrics(sess)
		if m == nil {
			return detailDataMsg{sessionID: sessionID}
		}

		return detailDataMsg{
			sessionID:    sessionID,
			messageCount: m.MessageCount,
			startTime:    m.StartTime,
		}
	}
}

// handleDetailKey closes the detail overlay on any key press.
func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.detail.Hide()
	m.mode = ModeList
	m.statusbar.SetMode(ModeList.String())
	return m, nil
}
