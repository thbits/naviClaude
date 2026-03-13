package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// claudeStatsCache is the structure of ~/.claude/stats-cache.json.
type claudeStatsCache struct {
	TotalSessions    int               `json:"totalSessions"`
	TotalMessages    int               `json:"totalMessages"`
	FirstSessionDate string            `json:"firstSessionDate"`
	DailyActivity    []dailyActivity   `json:"dailyActivity"`
	ModelUsage       map[string]modelUsageRaw `json:"modelUsage"`
	LongestSession   longestSessionRaw `json:"longestSession"`
	HourCounts       map[string]int    `json:"hourCounts"`
	LastComputedDate string            `json:"lastComputedDate"`
}

type dailyActivity struct {
	Date         string `json:"date"`
	MessageCount int    `json:"messageCount"`
	SessionCount int    `json:"sessionCount"`
	ToolCallCount int   `json:"toolCallCount"`
}

type modelUsageRaw struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	CacheRead    int `json:"cacheRead"`
	CacheWrite   int `json:"cacheWrite"`
}

type longestSessionRaw struct {
	SessionID    string `json:"sessionId"`
	Duration     int64  `json:"duration"` // milliseconds
	MessageCount int    `json:"messageCount"`
}

// Compute builds Stats by reading Claude's stats-cache.json and scanning project dirs.
func Compute(claudeDir string, activeCount int, filter string) (*Stats, error) {
	if claudeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		claudeDir = filepath.Join(home, ".claude")
	}

	st := &Stats{
		ActiveCount: activeCount,
		Filter:      filter,
	}

	// Read Claude's pre-computed stats cache.
	cacheFile := filepath.Join(claudeDir, "stats-cache.json")
	raw, err := os.ReadFile(cacheFile)
	if err == nil {
		var cc claudeStatsCache
		if err := json.Unmarshal(raw, &cc); err == nil {
			st.TotalSessions = cc.TotalSessions
			st.TotalMessages = cc.TotalMessages

			// Compute avg sessions per day.
			if cc.FirstSessionDate != "" {
				first, err := time.Parse("2006-01-02", cc.FirstSessionDate)
				if err == nil {
					days := time.Since(first).Hours() / 24
					if days > 0 {
						st.AvgSessionsPerDay = float64(cc.TotalSessions) / days
					}
				}
			}

			// Weekly activity (last 7 days).
			st.WeeklyActivity = filterDailyActivity(cc.DailyActivity, filter)
			if filter == "today" {
				st.HourlyActivity = buildHourlyActivity(claudeDir)
			}

			// Model usage grouped by family.
			st.ModelUsage = groupModelUsage(cc.ModelUsage)

			// Longest session.
			if cc.LongestSession.SessionID != "" {
				st.LongestSession = LongestInfo{
					SessionID:    cc.LongestSession.SessionID,
					DurationMins: int(cc.LongestSession.Duration / 60000),
					MessageCount: cc.LongestSession.MessageCount,
				}
			}

			// Peak hour.
			st.PeakHour = findPeakHour(cc.HourCounts)

			// Supplement if stale.
			if cc.LastComputedDate != "" {
				lastDate, err := time.Parse("2006-01-02", cc.LastComputedDate)
				if err == nil && time.Since(lastDate) > 24*time.Hour {
					supplementStaleData(st, claudeDir, lastDate)
				}
			}
		}
	}

	// Scan project directories for project breakdown.
	st.ProjectCounts = scanProjectCounts(claudeDir, filter)

	return st, nil
}

// CountSessionFiles returns the total number of .jsonl files across all project dirs.
func CountSessionFiles(claudeDir string) int {
	if claudeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0
		}
		claudeDir = filepath.Join(home, ".claude")
	}
	pattern := filepath.Join(claudeDir, "projects", "*", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return 0
	}
	return len(files)
}

func filterDailyActivity(daily []dailyActivity, filter string) []DayActivity {
	now := time.Now()

	// Build a map of all available daily data.
	dayMap := make(map[string]DayActivity)
	for _, d := range daily {
		dayMap[d.Date] = DayActivity{
			Date:         d.Date,
			MessageCount: d.MessageCount,
			SessionCount: d.SessionCount,
		}
	}

	switch filter {
	case "today":
		key := now.Format("2006-01-02")
		if da, ok := dayMap[key]; ok {
			return []DayActivity{da}
		}
		return []DayActivity{{Date: key}}

	case "week":
		// Last 7 days.
		var result []DayActivity
		for i := 6; i >= 0; i-- {
			d := now.AddDate(0, 0, -i)
			key := d.Format("2006-01-02")
			if da, ok := dayMap[key]; ok {
				result = append(result, da)
			} else {
				result = append(result, DayActivity{Date: key})
			}
		}
		return result

	default: // "all" -- last 30 days for the chart
		var result []DayActivity
		for i := 29; i >= 0; i-- {
			d := now.AddDate(0, 0, -i)
			key := d.Format("2006-01-02")
			if da, ok := dayMap[key]; ok {
				result = append(result, da)
			} else {
				result = append(result, DayActivity{Date: key})
			}
		}
		return result
	}
}

func groupModelUsage(usage map[string]modelUsageRaw) []ModelUsageEntry {
	families := map[string]int{
		"opus":   0,
		"sonnet": 0,
		"haiku":  0,
	}

	for modelID, u := range usage {
		lower := strings.ToLower(modelID)
		total := u.InputTokens + u.OutputTokens
		switch {
		case strings.Contains(lower, "opus"):
			families["opus"] += total
		case strings.Contains(lower, "sonnet"):
			families["sonnet"] += total
		case strings.Contains(lower, "haiku"):
			families["haiku"] += total
		}
	}

	var result []ModelUsageEntry
	for family, count := range families {
		if count > 0 {
			result = append(result, ModelUsageEntry{Family: family, Count: count})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})
	return result
}

func findPeakHour(hourCounts map[string]int) int {
	maxCount := 0
	peakHour := 0
	for hourStr, count := range hourCounts {
		if count > maxCount {
			maxCount = count
			// Parse hour string to int.
			h := 0
			for _, c := range hourStr {
				if c >= '0' && c <= '9' {
					h = h*10 + int(c-'0')
				}
			}
			peakHour = h
		}
	}
	return peakHour
}

func supplementStaleData(st *Stats, claudeDir string, lastDate time.Time) {
	// Count recent .jsonl files by modification date to supplement daily activity.
	pattern := filepath.Join(claudeDir, "projects", "*", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return
	}

	type dayStats struct {
		sessions int
		messages int // estimated from file count (each session ~ some messages)
	}
	dayCounts := make(map[string]*dayStats)
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.ModTime().After(lastDate) {
			day := info.ModTime().Format("2006-01-02")
			if dayCounts[day] == nil {
				dayCounts[day] = &dayStats{}
			}
			dayCounts[day].sessions++
			// Estimate messages from file size (roughly 1 message per 2KB).
			msgs := int(info.Size() / 2048)
			if msgs < 1 {
				msgs = 1
			}
			dayCounts[day].messages += msgs
		}
	}

	// Merge into weekly activity.
	dayMap := make(map[string]*DayActivity)
	for i := range st.WeeklyActivity {
		dayMap[st.WeeklyActivity[i].Date] = &st.WeeklyActivity[i]
	}
	for day, ds := range dayCounts {
		if da, ok := dayMap[day]; ok {
			if da.SessionCount == 0 {
				da.SessionCount = ds.sessions
			}
			if da.MessageCount == 0 {
				da.MessageCount = ds.messages
			}
		}
	}

	// Also supplement total counts.
	for _, ds := range dayCounts {
		st.TotalSessions += ds.sessions
		st.TotalMessages += ds.messages
	}
}

func buildHourlyActivity(claudeDir string) [24]int {
	var hours [24]int
	today := time.Now().Format("2006-01-02")
	pattern := filepath.Join(claudeDir, "projects", "*", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return hours
	}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.ModTime().Format("2006-01-02") == today {
			hours[info.ModTime().Hour()]++
		}
	}
	return hours
}

func scanProjectCounts(claudeDir string, filter string) []ProjectCount {
	now := time.Now()
	var cutoff time.Time
	switch filter {
	case "today":
		cutoff = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "week":
		cutoff = now.AddDate(0, 0, -7)
	}

	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}

	var counts []ProjectCount
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pattern := filepath.Join(projectsDir, e.Name(), "*.jsonl")
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		count := 0
		for _, f := range files {
			if cutoff.IsZero() {
				count++
			} else {
				info, err := os.Stat(f)
				if err == nil && info.ModTime().After(cutoff) {
					count++
				}
			}
		}
		if count == 0 {
			continue
		}

		// Convert slug back to a readable name: take the last path component.
		name := e.Name()
		parts := strings.Split(name, "-")
		if len(parts) > 1 {
			name = parts[len(parts)-1]
		}
		if name == "" {
			name = e.Name()
		}

		counts = append(counts, ProjectCount{Name: name, Count: count})
	}

	sort.Slice(counts, func(i, j int) bool {
		return counts[i].Count > counts[j].Count
	})

	// Top 10.
	if len(counts) > 10 {
		counts = counts[:10]
	}
	return counts
}
