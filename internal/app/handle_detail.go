package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

// loadDetailDataCmd reads the .jsonl file to count conversational turns and
// extract the start time. It delegates to session.LoadMetrics so the detail
// popup and the preview header report the same message count.
func loadDetailDataCmd(sess *session.Session) tea.Cmd {
	sessionID := sess.ID
	return func() tea.Msg {
		filePath := sess.SessionFile
		if filePath == "" && sess.ID != "" && sess.CWD != "" {
			filePath = session.SessionFilePath(sess.ID, sess.CWD)
		}
		if filePath == "" {
			return detailDataMsg{sessionID: sessionID}
		}

		// Model family only affects the context limit, which the detail popup
		// does not use, so the default ("") is fine here.
		m, err := session.LoadMetrics(filePath, "")
		if err != nil {
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
