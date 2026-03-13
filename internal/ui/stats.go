package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/thbits/naviClaude/internal/stats"
	"github.com/thbits/naviClaude/internal/styles"
)

const (
	FilterAll   = 0
	FilterWeek  = 1
	FilterToday = 2
)

// StatsModel renders a centered popup with usage statistics.
type StatsModel struct {
	visible bool
	loading bool
	stats   *stats.Stats
	filter  int // FilterAll, FilterWeek, FilterToday
	width   int
	height  int
}

// NewStats creates a StatsModel.
func NewStats() StatsModel {
	return StatsModel{}
}

// Show displays the stats popup.
func (m *StatsModel) Show() {
	m.visible = true
	m.loading = true
}

// Hide hides the stats popup.
func (m *StatsModel) Hide() {
	m.visible = false
	m.loading = false
}

// IsVisible returns whether the overlay is visible.
func (m *StatsModel) IsVisible() bool {
	return m.visible
}

// IsLoading returns whether stats are still being computed.
func (m *StatsModel) IsLoading() bool {
	return m.loading
}

// SetStats sets the computed stats and clears the loading state.
func (m *StatsModel) SetStats(s *stats.Stats) {
	m.stats = s
	m.loading = false
}

// SetSize updates the overlay container dimensions.
func (m *StatsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Filter returns the current filter index.
func (m *StatsModel) Filter() int {
	return m.filter
}

// CycleFilter advances to the next filter.
func (m *StatsModel) CycleFilter() {
	m.filter = (m.filter + 1) % 3
	m.loading = true
}

// FilterString returns the filter as a string for the compute function.
func (m *StatsModel) FilterString() string {
	switch m.filter {
	case FilterWeek:
		return "week"
	case FilterToday:
		return "today"
	default:
		return "all"
	}
}

// View renders the centered stats overlay.
func (m StatsModel) View() string {
	if !m.visible {
		return ""
	}

	if m.loading {
		content := styles.StatsTitle.Render("Statistics") + "\n\n" +
			styles.DetailLabel.Render("  Computing...")
		popup := styles.StatsBorder.Render(content)
		return popup
	}

	if m.stats == nil {
		content := styles.StatsTitle.Render("Statistics") + "\n\n" +
			styles.DetailLabel.Render("  No data available.")
		popup := styles.StatsBorder.Render(content)
		return popup
	}

	s := m.stats
	var lines []string

	// Title + filter badge.
	filterBadge := m.renderFilterBadge()
	lines = append(lines, styles.StatsTitle.Render("Statistics")+"  "+filterBadge)
	lines = append(lines, "")

	// Metrics row.
	metrics := fmt.Sprintf(
		"%s sessions  %s messages  %s active  %s avg/day",
		styles.StatsMetricValue.Render(fmt.Sprintf("%d", s.TotalSessions)),
		styles.StatsMetricValue.Render(fmt.Sprintf("%d", s.TotalMessages)),
		styles.StatsMetricValue.Render(fmt.Sprintf("%d", s.ActiveCount)),
		styles.StatsMetricValue.Render(fmt.Sprintf("%.1f", s.AvgSessionsPerDay)),
	)
	lines = append(lines, metrics)
	lines = append(lines, "")

	// Top projects with horizontal bars.
	if len(s.ProjectCounts) > 0 {
		lines = append(lines, styles.DetailLabel.Render("Top Projects"))
		maxCount := s.ProjectCounts[0].Count
		maxBarWidth := 30
		for _, p := range s.ProjectCounts {
			barWidth := 1
			if maxCount > 0 {
				barWidth = p.Count * maxBarWidth / maxCount
				if barWidth < 1 {
					barWidth = 1
				}
			}
			bar := strings.Repeat("\u2588", barWidth)
			name := fmt.Sprintf("%-12s", truncate(p.Name, 12))
			countStr := fmt.Sprintf(" %d", p.Count)
			lines = append(lines, "  "+styles.DetailLabel.Render(name)+" "+styles.StatsBar.Render(bar)+styles.DetailValue.Render(countStr))
		}
		lines = append(lines, "")
	}

	// Activity bar chart.
	if len(s.WeeklyActivity) > 0 {
		chartLabel := "Activity"
		if s.Filter == "week" {
			chartLabel = "Weekly Activity"
		} else if s.Filter == "today" {
			chartLabel = "Today's Activity"
		}
		lines = append(lines, styles.DetailLabel.Render(chartLabel))
		maxMsgs := 0
		for _, d := range s.WeeklyActivity {
			if d.MessageCount > maxMsgs {
				maxMsgs = d.MessageCount
			}
		}
		maxBarHeight := 6
		// Use single-char bars for 30-day view, double for 7-day.
		barChar := "\u2588"
		spaceChar := " "
		sepChar := ""
		if len(s.WeeklyActivity) <= 7 {
			barChar = "\u2588\u2588"
			spaceChar = "  "
			sepChar = " "
		}
		for row := maxBarHeight; row >= 1; row-- {
			var rowStr strings.Builder
			rowStr.WriteString("  ")
			for _, d := range s.WeeklyActivity {
				barHeight := 0
				if maxMsgs > 0 {
					barHeight = d.MessageCount * maxBarHeight / maxMsgs
				}
				if barHeight >= row {
					rowStr.WriteString(styles.StatsBar.Render(barChar))
				} else {
					rowStr.WriteString(spaceChar)
				}
				rowStr.WriteString(sepChar)
			}
			lines = append(lines, rowStr.String())
		}
		// Day labels -- show subset for 30-day view.
		var dayLabels strings.Builder
		dayLabels.WriteString("  ")
		for i, d := range s.WeeklyActivity {
			label := d.Date
			if len(s.WeeklyActivity) <= 7 {
				// Weekly: show 2-letter day name.
				if len(label) >= 10 {
					t, err := time.Parse("2006-01-02", label)
					if err == nil {
						label = t.Format("Mon")[:2]
					} else {
						label = label[8:10]
					}
				}
				dayLabels.WriteString(fmt.Sprintf("%-3s", label))
			} else {
				// 30-day: show day number every 7 days, space otherwise.
				if i == 0 || i == len(s.WeeklyActivity)-1 || i%7 == 0 {
					if len(label) >= 10 {
						dayLabels.WriteString(label[8:10])
					} else {
						dayLabels.WriteString(" ")
					}
				} else {
					dayLabels.WriteString(" ")
				}
			}
		}
		lines = append(lines, styles.DetailLabel.Render(dayLabels.String()))
		lines = append(lines, "")
	}

	// Model usage.
	if len(s.ModelUsage) > 0 {
		lines = append(lines, styles.DetailLabel.Render("Model Usage"))
		totalTokens := 0
		for _, mu := range s.ModelUsage {
			totalTokens += mu.Count
		}
		for _, mu := range s.ModelUsage {
			pct := 0
			if totalTokens > 0 {
				pct = mu.Count * 100 / totalTokens
			}
			barWidth := mu.Count * 30 / maxInt(totalTokens, 1)
			if barWidth < 1 && mu.Count > 0 {
				barWidth = 1
			}
			bar := strings.Repeat("\u2588", barWidth)
			barStyle := styles.StatsBar
			if mu.Family == "opus" {
				barStyle = styles.StatsBarAlt
			}
			label := fmt.Sprintf("%-8s", mu.Family)
			lines = append(lines, "  "+styles.DetailLabel.Render(label)+" "+barStyle.Render(bar)+" "+styles.DetailValue.Render(fmt.Sprintf("%d%%", pct)))
		}
		lines = append(lines, "")
	}

	// Longest session + peak hour.
	if s.LongestSession.SessionID != "" {
		dur := formatMinutes(s.LongestSession.DurationMins)
		lines = append(lines, styles.DetailLabel.Render("Longest session: ")+
			styles.DetailValue.Render(fmt.Sprintf("%s (%d messages)", dur, s.LongestSession.MessageCount)))
	}
	lines = append(lines, styles.DetailLabel.Render("Peak hour: ")+
		styles.DetailValue.Render(fmt.Sprintf("%02d:00", s.PeakHour)))

	lines = append(lines, "")
	lines = append(lines, styles.DetailLabel.Render("  Data from Claude Code's stats since installation"))
	lines = append(lines, "")
	lines = append(lines, styles.DetailLabel.Render("Tab")+" "+styles.HelpDesc.Render("filter")+"  "+
		styles.DetailLabel.Render("Any key")+" "+styles.HelpDesc.Render("close"))

	content := strings.Join(lines, "\n")
	popup := styles.StatsBorder.Render(content)

	return popup
}

func (m StatsModel) renderFilterBadge() string {
	filters := []struct {
		label string
		idx   int
	}{
		{"All time", FilterAll},
		{"This week", FilterWeek},
		{"Today", FilterToday},
	}

	var parts []string
	for _, f := range filters {
		if f.idx == m.filter {
			parts = append(parts, styles.StatsFilterActive.Render("["+f.label+"]"))
		} else {
			parts = append(parts, styles.StatsFilterInactive.Render(f.label))
		}
	}
	return strings.Join(parts, " ")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "\u2026"
}

func formatMinutes(mins int) string {
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dh %dm", mins/60, mins%60)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
