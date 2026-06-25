package app

import (
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/stats"
)

// tickResourceMsg triggers a CPU/memory refresh.
type tickResourceMsg struct{}

// resourceStats holds CPU and memory for a single PID.
type resourceStats struct {
	CPU   float64
	MemMB float64
}

// resourceRefreshMsg carries updated stats for active session PIDs.
type resourceRefreshMsg struct {
	stats map[int]resourceStats
}

// tickResource returns a 5-second ticker for resource stats.
func tickResource() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickResourceMsg{}
	})
}

// refreshResourceCmd builds a process tree and collects stats for active PIDs.
func (m Model) refreshResourceCmd() tea.Cmd {
	// Snapshot the active sessions' PIDs so the goroutine doesn't race.
	type pidInfo struct {
		pid    int
		target string
	}
	var pids []pidInfo
	for _, s := range m.activeSessions {
		if s.PID > 0 {
			pids = append(pids, pidInfo{pid: s.PID, target: s.TmuxTarget})
		}
	}
	if len(pids) == 0 {
		return nil
	}

	return func() tea.Msg {
		tree := session.BuildProcessTree()
		stats := make(map[int]resourceStats, len(pids))
		for _, p := range pids {
			cpu, mem := tree.Stats(p.pid)
			stats[p.pid] = resourceStats{CPU: cpu, MemMB: mem}
		}
		return resourceRefreshMsg{stats: stats}
	}
}

// handleResourceRefresh applies updated CPU/memory stats to active sessions.
func (m *Model) handleResourceRefresh(msg resourceRefreshMsg) {
	for _, s := range m.activeSessions {
		if st, ok := msg.stats[s.PID]; ok {
			s.CPU = st.CPU
			s.Memory = st.MemMB
		}
	}
	// Also update in m.sessions (may overlap).
	for _, s := range m.sessions {
		if s.PID > 0 {
			if st, ok := msg.stats[s.PID]; ok {
				s.CPU = st.CPU
				s.Memory = st.MemMB
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Stats popup
// ---------------------------------------------------------------------------

// statsComputeMsg carries the result of an async stats computation.
type statsComputeMsg struct {
	stats *stats.Stats
	err   error
}

// statsCacheKey combines the session-file count with a freshness fingerprint
// (the latest .jsonl modification time, truncated to seconds) so the stats
// cache is invalidated when existing files grow even though the file count is
// unchanged. The stats.Cache compares this value for equality; folding the
// freshness signal into the int it stores keeps the cache benefit for the
// truly-unchanged case (same count and same newest mtime) without editing the
// stats package.
func statsCacheKey() int {
	fileCount := stats.CountSessionFiles("")

	home, err := os.UserHomeDir()
	if err != nil {
		return fileCount
	}
	pattern := filepath.Join(home, ".claude", "projects", "*", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fileCount
	}
	var maxMod int64
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			if mod := info.ModTime().Unix(); mod > maxMod {
				maxMod = mod
			}
		}
	}
	// Combine count and newest mtime into one comparison key. The shift keeps the
	// file count from colliding with the mtime component for any realistic count.
	return fileCount + int(maxMod<<20)
}

// computeStatsCmd runs stats.Compute in the background.
func (m Model) computeStatsCmd() tea.Cmd {
	activeCount := len(m.activeSessions)
	filter := m.statsModel.FilterString()
	cache := m.statsCache

	return func() tea.Msg {
		// Check cache first. The key incorporates both the file count and the
		// newest .jsonl mtime so content growth invalidates a stale entry.
		cacheKey := statsCacheKey()
		if cached := cache.Load(cacheKey); cached != nil && cached.Filter == filter {
			return statsComputeMsg{stats: cached}
		}

		s, err := stats.Compute("", activeCount, filter)
		if err != nil {
			return statsComputeMsg{err: err}
		}

		// Save to cache.
		_ = cache.Save(s, cacheKey)

		return statsComputeMsg{stats: s}
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
