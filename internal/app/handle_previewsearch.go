package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// openPreviewSearch opens the query input over the preview.
func (m *Model) openPreviewSearch() {
	m.preview.OpenSearch()
	m.previewSearchSel = m.selectionIdentity()
	m.statusbar.SetPreviewSearch(false)
	m.mode = ModePreviewSearch
	m.statusbar.SetMode(ModePreviewSearch.String())
}

// clearPreviewSearch drops the preview search, leaving its input mode if open.
func (m *Model) clearPreviewSearch() {
	m.preview.ClearSearch()
	m.statusbar.SetPreviewSearch(false)
	if m.mode == ModePreviewSearch {
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
	}
}

// handlePreviewSearchKey handles keys while the preview's query input is open.
func (m Model) handlePreviewSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case KeyMenuCancel:
		m.clearPreviewSearch()

	case KeyEnter:
		// Back to the list with the matches kept; n / N walk them from there.
		m.statusbar.SetPreviewSearch(m.preview.CommitSearch())
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())

	case "down", "ctrl+n":
		m.preview.StepMatch(1)

	case "up", "ctrl+p":
		m.preview.StepMatch(-1)

	default:
		m.preview.TypeSearch(msg)
	}
	return m, nil
}
