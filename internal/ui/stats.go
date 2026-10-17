package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/stats"
	"github.com/thbits/naviClaude/internal/styles"
)

const (
	FilterAll   = 0
	FilterWeek  = 1
	FilterToday = 2
)

// Stats chart dimensions. Named so the layout math reads clearly; values are
// chosen to fit comfortably inside the stats popup border.
const (
	statsBarWidth        = 30 // max width (cells) of the horizontal project/model bars
	statsHourlyBarHeight = 4  // max height (rows) of the per-hour activity chart
	statsWeeklyBarHeight = 6  // max height (rows) of the weekly activity chart
	statsNameColMax      = 32 // cap on the project-name column width
	statsModelLabelWidth = 8  // fixed width (cells) of the model-family label column
	statsPopupChrome     = 6  // border plus horizontal padding around the content
)

// statsFilterLabels is indexed by FilterAll / FilterWeek / FilterToday.
var statsFilterLabels = []string{"All time", "This week", "Today"}

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

// CycleFilter advances to the next filter.
func (m *StatsModel) CycleFilter() {
	m.filter = (m.filter + 1) % 3
	m.loading = true
}

// SetFilter selects filter f, reporting whether it changed (and a recompute
// is needed).
func (m *StatsModel) SetFilter(f int) bool {
	if f < FilterAll || f > FilterToday || f == m.filter {
		return false
	}
	m.filter = f
	m.loading = true
	return true
}

// FilterAt returns the filter whose label is drawn at popup-local (x, y). It
// locates the labels in the rendered title line, so it tracks View exactly.
func (m StatsModel) FilterAt(x, y int) (int, bool) {
	lines := strings.Split(m.View(), "\n")
	if y < 0 || y >= len(lines) {
		return 0, false
	}
	line := ansi.Strip(lines[y])
	if !strings.Contains(line, "Statistics") {
		return 0, false
	}
	for f, label := range statsFilterLabels {
		if f == m.filter {
			label = "[" + label + "]"
		}
		i := strings.Index(line, label)
		if i < 0 {
			continue
		}
		start := ansi.StringWidth(line[:i])
		if x >= start && x < start+ansi.StringWidth(label) {
			return f, true
		}
	}
	return 0, false
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
		return styles.StatsBorder.Render(content)
	}

	if m.stats == nil {
		content := styles.StatsTitle.Render("Statistics") + "\n\n" +
			styles.DetailLabel.Render("  No data available.")
		return styles.StatsBorder.Render(content)
	}

	s := m.stats
	var lines []string
	lines = append(lines, styles.StatsTitle.Render("Statistics")+"  "+m.renderFilterBadge())
	if p := statsPeriodLabel(s); p != "" {
		lines = append(lines, styles.DetailLabel.Render(p))
	}
	lines = append(lines, "")

	value := func(v string) string { return styles.StatsMetricValue.Render(v) }
	label := func(v string) string { return styles.DetailLabel.Render(v) }
	counts := value(statsCount(s.Sessions)) + label(" sessions   ") +
		value(statsCount(s.Prompts)) + label(" prompts")
	if s.ActiveCount > 0 {
		counts += label("   ") + value(statsCount(s.ActiveCount)) + label(" running")
	}
	lines = append(lines, counts)
	lines = append(lines, value(statsTokens(s.Tokens.Output))+label(" output tokens   ")+
		value(statsTokens(s.Tokens.Total()))+label(" total"))
	lines = append(lines, label("tokens include cache reads/writes and subagents"))
	lines = append(lines, "")

	if s.Sessions == 0 && s.Tokens.Total() == 0 {
		lines = append(lines, label("No activity in this period."), "")
	} else {
		lines = append(lines, m.renderProjects(s)...)
		lines = append(lines, m.renderActivity(s)...)
		lines = append(lines, m.renderModels(s)...)
		if s.BusiestHour >= 0 {
			n := s.Hourly[s.BusiestHour]
			lines = append(lines, label("Busiest hour  ")+styles.DetailValue.Render(fmt.Sprintf(
				"%02d:00-%02d:00", s.BusiestHour, (s.BusiestHour+1)%24))+label(fmt.Sprintf("  %s %s", statsCount(n), statsPlural(n, "prompt"))), "")
		}
	}

	lines = append(lines, styles.DetailLabel.Render("Tab")+" "+styles.HelpDesc.Render("filter")+"  "+
		styles.DetailLabel.Render("Any key")+" "+styles.HelpDesc.Render("close"))

	return styles.StatsBorder.Render(strings.Join(lines, "\n"))
}

// statsPeriodLabel describes the span the numbers cover. "All time" is bounded by
// the transcripts Claude Code still keeps on disk.
func statsPeriodLabel(s *stats.Stats) string {
	if s.Since.IsZero() {
		return ""
	}
	since := s.Since.Local()
	switch s.Filter {
	case "today":
		return since.Format("Mon, Jan 2")
	case "week":
		return since.Format("Jan 2") + " - " + time.Now().Format("Jan 2")
	}
	return "all transcripts on disk, since " + since.Format("Jan 2, 2006")
}

func (m StatsModel) renderProjects(s *stats.Stats) []string {
	if len(s.Projects) == 0 {
		return nil
	}
	lines := []string{styles.DetailLabel.Render("Top projects") + styles.HelpDesc.Render("  sessions / prompts")}
	nameWidth, countWidth := 0, 0
	counts := make([]string, len(s.Projects))
	for i, p := range s.Projects {
		nameWidth = max(nameWidth, ansi.StringWidth(p.Name))
		counts[i] = fmt.Sprintf(" / %s", statsCount(p.Prompts))
		countWidth = max(countWidth, len(fmt.Sprintf(" %d", p.Sessions))+len(counts[i]))
	}
	nameWidth = min(nameWidth, statsNameColMax)
	// Shrink the name column rather than let the popup outgrow the terminal.
	if m.width > 0 {
		nameWidth = max(min(nameWidth, m.width-statsPopupChrome-2-1-statsBarWidth-countWidth), 8)
	}
	maxSessions := max(s.Projects[0].Sessions, 1)
	for i, p := range s.Projects {
		bar := strings.Repeat("\u2588", max(p.Sessions*statsBarWidth/maxSessions, 1))
		name := fmt.Sprintf("%-*s ", nameWidth, truncateDisplayLeft(p.Name, nameWidth))
		lines = append(lines, "  "+styles.DetailLabel.Render(name)+styles.StatsBar.Render(bar)+
			styles.DetailValue.Render(fmt.Sprintf(" %d", p.Sessions))+styles.DetailLabel.Render(counts[i]))
	}
	return append(lines, "")
}

func (m StatsModel) renderActivity(s *stats.Stats) []string {
	var values []int
	var labels []statsAxisLabel
	var title string
	barW, gap := 1, 1
	switch {
	case s.Filter == "today":
		title = "Prompts by hour"
		values = s.Hourly[:]
		for _, h := range []int{0, 23, 6, 12, 18} {
			labels = append(labels, statsAxisLabel{bar: h, text: fmt.Sprintf("%d", h)})
		}
	case len(s.Daily) > 0:
		title = "Prompts per day"
		if s.Filter != "week" {
			title += ", last " + fmt.Sprintf("%d", len(s.Daily)) + " days"
		}
		for _, d := range s.Daily {
			values = append(values, d.Prompts)
		}
		last := len(s.Daily) - 1
		if len(s.Daily) <= 7 {
			barW, gap = 3, 2
			for i := last; i >= 0; i-- {
				labels = append(labels, statsAxisLabel{bar: i, text: statsDayLabel(s.Daily[i].Date, "Mon")})
			}
		} else {
			labels = append(labels, statsAxisLabel{bar: last, text: statsDayLabel(s.Daily[last].Date, "Jan 2")},
				statsAxisLabel{bar: 0, text: statsDayLabel(s.Daily[0].Date, "Jan 2")})
			for i := last - 7; i > 0; i -= 7 {
				labels = append(labels, statsAxisLabel{bar: i, text: statsDayLabel(s.Daily[i].Date, "Jan 2")})
			}
		}
	default:
		return nil
	}
	peak := 0
	for _, v := range values {
		peak = max(peak, v)
	}
	if peak == 0 {
		return []string{styles.DetailLabel.Render(title) + styles.HelpDesc.Render("  none"), ""}
	}
	height := statsWeeklyBarHeight
	if s.Filter == "today" {
		height = statsHourlyBarHeight
	}
	lines := []string{styles.DetailLabel.Render(title) + styles.HelpDesc.Render(fmt.Sprintf("  peak %s", statsCount(peak)))}
	for _, row := range statsBars(values, barW, gap, height) {
		lines = append(lines, "  "+styles.StatsBar.Render(row))
	}
	width := len(values)*(barW+gap) - gap
	lines = append(lines, "  "+styles.DetailLabel.Render(statsAxisLabels(width, barW+gap, labels)))
	return append(lines, "")
}

func (m StatsModel) renderModels(s *stats.Stats) []string {
	var total int64
	for _, mu := range s.Models {
		total += mu.Tokens
	}
	if total == 0 {
		return nil
	}
	lines := []string{styles.DetailLabel.Render("Models") + styles.HelpDesc.Render("  share of total tokens")}
	for _, mu := range s.Models {
		share := float64(mu.Tokens) / float64(total)
		pct := fmt.Sprintf("%.0f%%", share*100)
		barWidth := int(share*statsBarWidth + 0.5)
		if share < 0.005 {
			pct, barWidth = "<1%", 0
		} else {
			barWidth = max(barWidth, 1)
		}
		barStyle := styles.StatsBar
		if mu.Family == "opus" {
			barStyle = styles.StatsBarAlt
		}
		row := "  " + styles.DetailLabel.Render(fmt.Sprintf("%-*s", statsModelLabelWidth, capitalize(mu.Family))) + " "
		if barWidth > 0 {
			row += barStyle.Render(strings.Repeat("\u2588", barWidth)) + " "
		}
		lines = append(lines, row+styles.DetailValue.Render(pct)+styles.DetailLabel.Render("  "+statsTokens(mu.Tokens)))
	}
	return append(lines, "")
}

// statsBlocks are eighth-height blocks, so a bar's top cell shows its remainder.
var statsBlocks = []rune(" \u2581\u2582\u2583\u2584\u2585\u2586\u2587\u2588")

// statsBars draws a vertical bar chart top row first. Any non-zero value gets
// at least a sliver so small days stay visible.
func statsBars(values []int, barW, gap, height int) []string {
	peak := 0
	for _, v := range values {
		peak = max(peak, v)
	}
	units := make([]int, len(values))
	for i, v := range values {
		if peak > 0 && v > 0 {
			units[i] = max(v*height*8/peak, 1)
		}
	}
	rows := make([]string, height)
	for r := 0; r < height; r++ {
		level := height - 1 - r
		var b strings.Builder
		for i, u := range units {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", gap))
			}
			cell := min(max(u-level*8, 0), 8)
			b.WriteString(strings.Repeat(string(statsBlocks[cell]), barW))
		}
		rows[r] = b.String()
	}
	return rows
}

// statsAxisLabel is a label anchored at a bar index.
type statsAxisLabel struct {
	bar  int
	text string
}

// statsAxisLabels lays labels out in priority order on a width-cell row,
// skipping any that would touch an already placed label.
func statsAxisLabels(width, step int, labels []statsAxisLabel) string {
	row := []rune(strings.Repeat(" ", width))
	taken := make([]bool, width)
	for _, l := range labels {
		w := len([]rune(l.text))
		start := min(l.bar*step, width-w)
		if start < 0 {
			continue
		}
		free := true
		for c := max(start-1, 0); c < min(start+w+1, width); c++ {
			if taken[c] {
				free = false
				break
			}
		}
		if !free {
			continue
		}
		for i, r := range []rune(l.text) {
			row[start+i] = r
			taken[start+i] = true
		}
	}
	return string(row)
}

func statsDayLabel(date, layout string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.Format(layout)
}

func statsCount(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func statsTokens(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

func statsPlural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func (m StatsModel) renderFilterBadge() string {
	var parts []string
	for idx, label := range statsFilterLabels {
		if idx == m.filter {
			parts = append(parts, styles.StatsFilterActive.Render("["+label+"]"))
		} else {
			parts = append(parts, styles.StatsFilterInactive.Render(label))
		}
	}
	return strings.Join(parts, " ")
}
