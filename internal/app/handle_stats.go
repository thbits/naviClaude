package app

import (
	"fmt"
	"github.com/thbits/naviClaude/internal/harness"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/stats"
)

// ---------------------------------------------------------------------------
// Stats popup
// ---------------------------------------------------------------------------

// statsComputeMsg carries the result of an async stats computation.
type statsComputeMsg struct {
	stats *stats.Stats
	err   error
}

// statsProvider returns the first registered harness that computes stats.
func (m Model) statsProvider() (harness.StatsProvider, bool) {
	if m.registry == nil {
		return nil, false
	}
	for _, h := range m.registry.All() {
		if sp, ok := harness.AsStats(h); ok {
			return sp, true
		}
	}
	return nil, false
}

// computeStatsCmd computes stats in the background via the stats provider.
func (m Model) computeStatsCmd() tea.Cmd {
	activeCount := len(m.activeSessions)
	filter := m.statsModel.FilterString()
	sp, ok := m.statsProvider()

	return func() tea.Msg {
		if !ok {
			return statsComputeMsg{err: fmt.Errorf("no harness provides stats")}
		}
		s, err := sp.ComputeStats(activeCount, filter)
		return statsComputeMsg{stats: s, err: err}
	}
}

// handleStatsKey handles input in the stats overlay.
func (m Model) handleStatsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == KeyTab {
		m.statsModel.CycleFilter()
		return m, m.computeStatsCmd()
	}
	// Any other key closes.
	m.statsModel.Hide()
	m.mode = ModeList
	m.statusbar.SetMode(ModeList.String())
	return m, nil
}
