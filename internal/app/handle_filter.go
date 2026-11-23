package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/styles"
	"github.com/thbits/naviClaude/internal/ui"
)

// nextAttention moves the selection to the next session that needs the user
// (waiting, then done; the working ones when there are none), wrapping around.
func (m *Model) nextAttention() tea.Cmd {
	prev := m.selectionIdentity()
	moved, ring := m.sidebar.SelectNextAttention()
	if moved {
		return m.selectionMoved(prev)
	}
	notice := "no session is waiting, done or working"
	if ring > 0 {
		notice = "no other session is waiting, done or working"
	}
	if f := m.sidebar.StatusFilter(); f != ui.FilterNone {
		notice += " (filter: " + f.String() + ")"
	}
	m.statusbar.SetNotice(notice)
	return nil
}

// toggleStatusFilter turns status filter f on, or off when it is already on.
func (m *Model) toggleStatusFilter(f ui.StatusFilter) tea.Cmd {
	if m.sidebar.StatusFilter() == f {
		f = ui.FilterNone
	}
	return m.setStatusFilter(f)
}

// setStatusFilter applies status filter f and syncs the panes when the cursor
// had to leave a row the filter hides.
func (m *Model) setStatusFilter(f ui.StatusFilter) tea.Cmd {
	prev := m.selectionIdentity()
	m.sidebar.SetStatusFilter(f)
	if m.selectionIdentity() != prev {
		return m.afterSelectionChange()
	}
	return nil
}

// filterTag is the title bar's mark for an active status filter.
func filterTag(f ui.StatusFilter) string {
	return lipgloss.NewStyle().Foreground(styles.ColorBg).Background(styles.ColorAmber).Bold(true).
		Render(" filter: " + f.String() + " ")
}
