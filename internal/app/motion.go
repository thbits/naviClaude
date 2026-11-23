package app

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// maxCount caps a typed count so it can't overflow; no list is that long.
const maxCount = 9999

// countDigit reports the digit a key adds to a pending count: 1-9 start one and
// 0 only extends one, as in vim, where a bare 0 is a motion of its own.
func countDigit(msg tea.KeyMsg, count int) (int, bool) {
	if msg.Type != tea.KeyRunes || msg.Alt || msg.Paste || len(msg.Runes) != 1 {
		return 0, false
	}
	r := msg.Runes[0]
	if r < '0' || r > '9' || (r == '0' && count == 0) {
		return 0, false
	}
	return int(r - '0'), true
}

// addCountDigit extends the pending count and shows it in the status bar.
func (m *Model) addCountDigit(d int) {
	m.count = min(m.count*10+d, maxCount)
	m.statusbar.SetPending(strconv.Itoa(m.count))
}

// clearPending drops a pending count.
func (m *Model) clearPending() {
	m.count = 0
	m.statusbar.SetPending("")
}

// listMotion runs a counted or vim-style list motion; count is 0 when none was
// typed. Counts number sessions only, matching the shown numbers, while a bare
// j/k still steps onto group headers. It reports false for keys the sidebar handles itself.
func (m *Model) listMotion(key string, count int) bool {
	n := max(count, 1)
	switch key {
	case "j", "down":
		if count == 0 {
			m.sidebar.MoveCursor(1)
		} else {
			m.sidebar.MoveSessions(count)
		}
	case "k", "up":
		if count == 0 {
			m.sidebar.MoveCursor(-1)
		} else {
			m.sidebar.MoveSessions(-count)
		}
	case "g":
		if count == 0 {
			m.sidebar.GoToRow(1)
		} else {
			m.sidebar.GoToSession(count)
		}
	case "G":
		if count == 0 {
			return false
		}
		m.sidebar.GoToSession(count)
	case "}":
		m.sidebar.JumpGroup(n)
	case "{":
		m.sidebar.JumpGroup(-n)
	default:
		return false
	}
	return true
}

// toggleLineNumbers flips the row numbers and saves the choice to the config.
func (m *Model) toggleLineNumbers() {
	mode := "off"
	if m.cfg.LineNumbers == "off" || m.cfg.LineNumbers == "" {
		mode = m.lineNumbersOn
		if mode == "off" || mode == "" {
			mode = "relative" // the mode that shows the count each row needs
		}
	}
	m.cfg.LineNumbers = mode
	if mode != "off" {
		m.lineNumbersOn = mode
	}
	m.sidebar.SetLineNumbers(mode)
	if err := m.writeConfig(); err != nil {
		m.statusbar.SetError("row numbers changed for this run only (config write failed: " + err.Error() + ")")
		return
	}
	m.statusbar.SetNotice("row numbers " + mode)
}
