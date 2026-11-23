package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/styles"
)

// SetLineNumbers sets the session numbers: "off", "absolute" or "relative".
func (m *SidebarModel) SetLineNumbers(mode string) {
	if mode == m.lineNumbers {
		return
	}
	m.lineNumbers = mode
	m.syncViewport()
}

// LineNumbers reports the numbering shown: "off", "absolute" or "relative".
func (m *SidebarModel) LineNumbers() string {
	if m.lineNumbers == "" {
		return "off"
	}
	return m.lineNumbers
}

// SetPendingCount sets the vim count being typed ("" for none). While one is
// pending the numbers show even when off, and those it prefixes light up.
func (m *SidebarModel) SetPendingCount(p string) {
	if p == m.pendingCount {
		return
	}
	m.pendingCount = p
	m.syncViewport()
}

// numberMode is the numbering drawn now: the configured mode, else relative (the
// count j/k take) while a count is pending, else "".
func (m *SidebarModel) numberMode() string {
	switch {
	case m.lineNumbers == "absolute" || m.lineNumbers == "relative":
		return m.lineNumbers
	case m.pendingCount != "":
		return "relative"
	}
	return ""
}

// renderNumber renders a session's number right-aligned in 2 columns plus a gap:
// faint, or while a count is pending its typed prefix in amber and the rest in gray.
func (m *SidebarModel) renderNumber(num string, bg lipgloss.Color) string {
	base := lipgloss.NewStyle().Background(bg)
	pad := base.Render(strings.Repeat(" ", max(2-len(num), 0)))
	p := m.pendingCount
	gap := base.Render(" ")
	if p == "" || !strings.HasPrefix(num, p) {
		return pad + base.Foreground(styles.ColorDim).Render(num) + gap
	}
	out := pad + base.Foreground(styles.ColorAmber).Bold(true).Render(p)
	if rest := num[len(p):]; rest != "" {
		out += base.Foreground(styles.ColorGray).Render(rest)
	}
	return out + gap
}

// sessionOrdinals maps each flat item to the count of session rows up to and
// including it, so a session's ordinal is its 1-based number; nil when numbers are off.
func (m *SidebarModel) sessionOrdinals() []int {
	if m.numberMode() == "" {
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
	if m.numberMode() != "relative" || idx == m.cursor || m.cursor < 0 || m.cursor >= len(ord) {
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
