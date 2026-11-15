package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/styles"
)

// minRowWidth is the narrowest row worth giving up columns to a number gutter for.
const minRowWidth = 24

// SetLineNumbers sets the row-number gutter: "off", "absolute" or "relative".
func (m *SidebarModel) SetLineNumbers(mode string) {
	if mode == m.lineNumbers {
		return
	}
	m.lineNumbers = mode
	m.syncViewport()
}

// gutterWidth is the columns the row numbers take, digits plus a space; 0 when
// they are off or the pane is too narrow to spare them.
func (m *SidebarModel) gutterWidth() int {
	if m.lineNumbers != "absolute" && m.lineNumbers != "relative" || len(m.flatItems) == 0 {
		return 0
	}
	w := len(strconv.Itoa(len(m.flatItems))) + 1
	if m.width-w < minRowWidth {
		return 0
	}
	return w
}

// rowNumber is the label for flat item idx: its 1-based row, or in relative mode
// its distance from the cursor, the count j/k need to reach it.
func (m *SidebarModel) rowNumber(idx int) int {
	if m.lineNumbers == "relative" && idx != m.cursor {
		d := idx - m.cursor
		if d < 0 {
			d = -d
		}
		return d
	}
	return idx + 1
}

// addGutter prefixes each content line with its row's number on the row's first
// line and blanks elsewhere, using lineItems to know which row owns each line.
func (m *SidebarModel) addGutter(lines []string, gutter int) {
	numStyle := lipgloss.NewStyle().Foreground(styles.ColorGray)
	curStyle := lipgloss.NewStyle().Foreground(styles.ColorAmber).Bold(true)
	blank := strings.Repeat(" ", gutter)
	for i, owner := range m.lineItems {
		if owner < 0 || (i > 0 && m.lineItems[i-1] == owner) {
			lines[i] = blank + lines[i]
			continue
		}
		label := strconv.Itoa(m.rowNumber(owner))
		label = strings.Repeat(" ", gutter-1-len(label)) + label + " "
		style := numStyle
		if owner == m.cursor {
			style = curStyle
		}
		lines[i] = style.Render(label) + lines[i]
	}
}
