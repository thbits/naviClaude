package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/stats"
)

func statsView(t *testing.T, s *stats.Stats, filter int) []string {
	t.Helper()
	m := NewStats()
	m.Show()
	m.SetFilter(filter)
	m.SetStats(s)
	return strings.Split(ansi.Strip(m.View()), "\n")
}

func dailyStats(days int, end time.Time) []stats.DayActivity {
	var out []stats.DayActivity
	for i := days - 1; i >= 0; i-- {
		out = append(out, stats.DayActivity{Date: end.AddDate(0, 0, -i).Format("2006-01-02"), Prompts: i%5 + 1})
	}
	return out
}

// axisLine returns the row under the chart, which is the only block-drawn
// section in these fixtures.
func axisLine(t *testing.T, lines []string) string {
	t.Helper()
	last := -1
	for i, l := range lines {
		if strings.ContainsAny(l, "▁▂▃▄▅▆▇█") {
			last = i
		}
	}
	if last < 0 || last+1 >= len(lines) {
		t.Fatalf("chart not found in:\n%s", strings.Join(lines, "\n"))
	}
	return strings.Trim(lines[last+1], "│")
}

func TestStatsAxisLabelsDoNotCollide(t *testing.T) {
	end := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	tests := []struct {
		name    string
		filter  int
		st      *stats.Stats
		pattern string
		want    []string
	}{
		{"30 days", FilterAll, &stats.Stats{Filter: "all", Sessions: 1, Daily: dailyStats(30, end)},
			`[A-Z][a-z]{2} \d{1,2}`, []string{"Sep 18", "Oct 17"}},
		{"7 days", FilterWeek, &stats.Stats{Filter: "week", Sessions: 1, Daily: dailyStats(7, end)},
			`[A-Z][a-z]{2}`, []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}},
		{"24 hours", FilterToday, &stats.Stats{Filter: "today", Sessions: 1, Hourly: [24]int{0: 1, 23: 2}},
			`\d{1,2}`, []string{"0", "6", "12", "18", "23"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			axis := axisLine(t, statsView(t, tt.st, tt.filter))
			re := regexp.MustCompile(tt.pattern)
			var got []string
			prevEnd := -2
			for _, loc := range re.FindAllStringIndex(axis, -1) {
				if loc[0] <= prevEnd {
					t.Errorf("labels touch at %d in %q", loc[0], axis)
				}
				prevEnd = loc[1]
				got = append(got, axis[loc[0]:loc[1]])
			}
			if rest := strings.TrimSpace(re.ReplaceAllString(axis, "")); rest != "" {
				t.Errorf("axis has stray text %q: %q", rest, axis)
			}
			for _, w := range tt.want {
				if !strings.Contains(strings.Join(got, "|"), w) {
					t.Errorf("label %q missing from %q", w, axis)
				}
			}
		})
	}
}

func TestStatsAxisLabelsSkipOverlaps(t *testing.T) {
	labels := []statsAxisLabel{{bar: 9, text: "Oct 17"}, {bar: 8, text: "Oct 16"}, {bar: 0, text: "Oct 8"}}
	// "Oct 17" is clamped to end at the row edge; the others would touch it.
	if got, want := statsAxisLabels(10, 1, labels), "    Oct 17"; got != want {
		t.Errorf("statsAxisLabels = %q, want %q", got, want)
	}
}

func TestStatsModelsNoBarForTinyShare(t *testing.T) {
	st := &stats.Stats{Filter: "all", Sessions: 1, Models: []stats.ModelUsageEntry{
		{Family: "opus", Tokens: 9990}, {Family: "haiku", Tokens: 10},
	}}
	var opus, haiku string
	for _, l := range statsView(t, st, FilterAll) {
		switch {
		case strings.Contains(l, "Opus"):
			opus = l
		case strings.Contains(l, "Haiku"):
			haiku = l
		}
		if strings.Contains(l, "opus") || strings.Contains(l, "haiku") {
			t.Errorf("model names must be capitalized like the preview header: %q", l)
		}
	}
	if !strings.Contains(haiku, "<1%") || strings.Contains(haiku, "█") {
		t.Errorf("haiku row = %q, want <1%% without a bar", haiku)
	}
	if !strings.Contains(opus, "100%") || !strings.Contains(opus, "█") {
		t.Errorf("opus row = %q, want a bar and 100%%", opus)
	}
}

func TestStatsBarsUseEighths(t *testing.T) {
	got := statsBars([]int{0, 1, 8, 16}, 1, 0, 2)
	want := []string{"   █", " ▁██"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("statsBars = %q, want %q", got, want)
	}
}

func TestStatsViewOmitsDroppedMetrics(t *testing.T) {
	st := &stats.Stats{Filter: "all", Sessions: 3, Prompts: 1234, BusiestHour: 14,
		Tokens: stats.Tokens{Output: 2_500_000, CacheRead: 1_000_000_000}}
	st.Hourly[14] = 7
	view := strings.Join(statsView(t, st, FilterAll), "\n")
	for _, want := range []string{"3 sessions", "1,234 prompts", "2.5M output tokens", "1.00B total", "14:00-15:00  7 prompts"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	for _, gone := range []string{"avg/day", "Longest session", "messages"} {
		if strings.Contains(view, gone) {
			t.Errorf("view still shows %q", gone)
		}
	}
}

func TestStatsFilterAtTracksLabels(t *testing.T) {
	m := NewStats()
	m.Show()
	m.SetStats(&stats.Stats{Filter: "all", BusiestHour: -1})
	lines := strings.Split(m.View(), "\n")
	for y, l := range lines {
		plain := ansi.Strip(l)
		if !strings.Contains(plain, "Statistics") {
			continue
		}
		for f, label := range statsFilterLabels {
			i := strings.Index(plain, label)
			if got, ok := m.FilterAt(ansi.StringWidth(plain[:i])+1, y); !ok || got != f {
				t.Errorf("FilterAt(%q) = %d, %v; want %d", label, got, ok, f)
			}
		}
		return
	}
	t.Fatal("title line not found")
}

func TestStatsPopupFitsTerminalWidth(t *testing.T) {
	st := &stats.Stats{Filter: "all", Sessions: 70, Daily: dailyStats(30, time.Now()), Projects: []stats.ProjectCount{
		{Name: "claude-marketplace/skills/devops-pr-review", Sessions: 61, Prompts: 1434},
		{Name: ".agents/skills/devops-pr-review", Sessions: 9, Prompts: 95},
	}}
	m := NewStats()
	m.SetSize(80, 50)
	m.Show()
	m.SetStats(st)
	for _, l := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("line %d cells wide on an 80-column terminal: %q", w, ansi.Strip(l))
		}
	}
}
