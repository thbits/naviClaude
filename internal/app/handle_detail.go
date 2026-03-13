package app

import (
	"bufio"
	"encoding/json"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
)

// detailDataMsg carries data loaded from the session's .jsonl file.
type detailDataMsg struct {
	messageCount int
	startTime    time.Time
}

// loadDetailDataCmd reads the .jsonl file to count messages and extract start time.
func loadDetailDataCmd(sess *session.Session) tea.Cmd {
	return func() tea.Msg {
		filePath := sess.SessionFile
		if filePath == "" && sess.ID != "" && sess.CWD != "" {
			filePath = session.SessionFilePath(sess.ID, sess.CWD)
		}
		if filePath == "" {
			return detailDataMsg{}
		}

		f, err := os.Open(filePath)
		if err != nil {
			return detailDataMsg{}
		}
		defer f.Close()

		var (
			count     int
			startTime time.Time
		)

		scanner := bufio.NewScanner(f)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 4*1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var rec struct {
				Type      string `json:"type"`
				Timestamp string `json:"timestamp"`
			}
			if err := json.Unmarshal(line, &rec); err != nil {
				continue
			}

			// Count user and assistant messages.
			if rec.Type == "user" || rec.Type == "assistant" {
				count++
			}

			// Extract the earliest timestamp as start time.
			if rec.Timestamp != "" && startTime.IsZero() {
				t, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
				if err != nil {
					t, _ = time.Parse(time.RFC3339, rec.Timestamp)
				}
				if !t.IsZero() {
					startTime = t
				}
			}
		}

		return detailDataMsg{
			messageCount: count,
			startTime:    startTime,
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
