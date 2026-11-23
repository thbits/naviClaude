package ui

import "strconv"

// SetLineNumbers sets the session numbers: "off", "absolute" or "relative".
func (m *SidebarModel) SetLineNumbers(mode string) {
	if mode == m.lineNumbers {
		return
	}
	m.lineNumbers = mode
	m.syncViewport()
}

// sessionOrdinals maps each flat item to the count of session rows up to and
// including it, so a session's ordinal is its 1-based number; nil when numbers are off.
func (m *SidebarModel) sessionOrdinals() []int {
	if m.lineNumbers != "absolute" && m.lineNumbers != "relative" {
		return nil
	}
	ord := make([]int, len(m.flatItems))
	n := 0
	for i, it := range m.flatItems {
		if !it.isGroup {
			n++
		}
		ord[i] = n
	}
	return ord
}

// sessionNumber is the label for session row idx: its 1-based session number, or
// in relative mode the sessions between it and the cursor, the count j/k need.
// Headers and the off mode get "".
func (m *SidebarModel) sessionNumber(ord []int, idx int) string {
	if ord == nil || m.flatItems[idx].isGroup {
		return ""
	}
	if m.lineNumbers != "relative" || idx == m.cursor || m.cursor < 0 || m.cursor >= len(ord) {
		return strconv.Itoa(ord[idx])
	}
	if idx > m.cursor {
		return strconv.Itoa(ord[idx] - ord[m.cursor])
	}
	before := ord[m.cursor] // sessions in [idx, cursor), leaving out a session cursor
	if !m.flatItems[m.cursor].isGroup {
		before--
	}
	return strconv.Itoa(before - ord[idx] + 1)
}
