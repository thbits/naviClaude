package stats

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// claudeStatsCache is the structure of ~/.claude/stats-cache.json.
type claudeStatsCache struct {
	TotalSessions    int                      `json:"totalSessions"`
	TotalMessages    int                      `json:"totalMessages"`
	FirstSessionDate string                   `json:"firstSessionDate"`
	DailyActivity    []dailyActivity          `json:"dailyActivity"`
	ModelUsage       map[string]modelUsageRaw `json:"modelUsage"`
	LongestSession   longestSessionRaw        `json:"longestSession"`
	HourCounts       map[string]int           `json:"hourCounts"`
	LastComputedDate string                   `json:"lastComputedDate"`
}

type dailyActivity struct {
	Date          string `json:"date"`
	MessageCount  int    `json:"messageCount"`
	SessionCount  int    `json:"sessionCount"`
	ToolCallCount int    `json:"toolCallCount"`
}

type modelUsageRaw struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	// Claude's cache keys these as cacheReadInputTokens /
	// cacheCreationInputTokens. For Claude Code these dwarf input+output
	// (cache read/creation is ~99.9% of tokens), so usage counts them too.
	CacheRead  int `json:"cacheReadInputTokens"`
	CacheWrite int `json:"cacheCreationInputTokens"`
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

			// Compute avg sessions per day. firstSessionDate is a full RFC3339
			// timestamp in the real cache, so parseStatsDate tries that first
			// (a date-only parse would fail on the "T..." suffix and leave the
			// average at 0) before falling back to a local-zone date parse.
			if cc.FirstSessionDate != "" {
				first, err := parseStatsDate(cc.FirstSessionDate)
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

			// Supplement if stale. parseStatsDate handles both the date-only
			// form this field normally takes and the RFC3339 form, so the
			// staleness check stays consistent regardless of which Claude wrote.
			if cc.LastComputedDate != "" {
				lastDate, err := parseStatsDate(cc.LastComputedDate)
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

// parseStatsDate parses a date from Claude's stats cache, which is not
// self-consistent: firstSessionDate is a full RFC3339 timestamp
// ("2026-01-29T11:07:33.236Z") while lastComputedDate is date-only
// ("2026-02-16"). Try the timestamp form first, then fall back to a date-only
// parse in the local zone so the elapsed-days math against time.Now() (used by
// time.Since) lines up in one consistent zone.
func parseStatsDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02", s, time.Local)
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
		total := u.InputTokens + u.OutputTokens + u.CacheRead + u.CacheWrite
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
		// Parse hour string to int.
		h := 0
		for _, c := range hourStr {
			if c >= '0' && c <= '9' {
				h = h*10 + int(c-'0')
			}
		}
		// Iterating a Go map is nondeterministic, so a strict ">" would pick an
		// arbitrary winner among hours that tie on count. Break ties toward the
		// lowest hour so the result is stable across runs.
		if count > maxCount || (count == maxCount && count > 0 && h < peakHour) {
			maxCount = count
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
		// Track whether this day's sessions/messages were already represented
		// in the cached weekly/total data so we don't double-count them below.
		sessionsAlreadyCounted := false
		messagesAlreadyCounted := false
		if da, ok := dayMap[day]; ok {
			if da.SessionCount == 0 {
				da.SessionCount = ds.sessions
			} else {
				sessionsAlreadyCounted = true
			}
			if da.MessageCount == 0 {
				da.MessageCount = ds.messages
			} else {
				messagesAlreadyCounted = true
			}
		}

		// Also supplement total counts, but only with the days/metrics that were
		// not already present in the cached weekly/total data. Adding every day
		// unconditionally double-counts days that the cache already includes.
		if !sessionsAlreadyCounted {
			st.TotalSessions += ds.sessions
		}
		if !messagesAlreadyCounted {
			st.TotalMessages += ds.messages
		}
	}
}

func buildHourlyActivity(claudeDir string) [24]int {
	var hours [24]int
	// Use local time for both the "today" key and the per-file ModTime bucketing
	// so the day boundary is consistent (file ModTimes are reported in local
	// time). Keep the date format stable to match the rest of the package.
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
	// Build the cutoff from the local clock so it compares like-for-like against
	// file ModTimes (also local) below; mixing in a UTC-parsed boundary would
	// shift the day edge by the local UTC offset.
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

		// Prefer the real working directory recorded in the session files.
		// The directory slug is lossy -- Claude maps every "/" in the path to
		// "-", which is indistinguishable from "-" characters already in the
		// path -- so splitting it mangles names like "dev/us-east-1" into "1".
		name := projectNameFromCwd(cachedProjectCwd(e.Name(), files))
		if name == "" {
			// Fall back to the slug heuristic: the slug encodes the cwd path with
			// "/" rewritten to "-", so turning "-" back into "/" and taking the
			// last component recovers the final path segment (the same single
			// component the previous hand-rolled "-" split produced).
			name = filepath.Base(strings.ReplaceAll(e.Name(), "-", "/"))
			if name == "" || name == "." || name == "/" {
				name = e.Name()
			}
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

// projectNameFromCwd builds a readable project name from a working-directory
// path: the last up to two path components, joined with "/". Showing two
// components disambiguates projects whose final component is shared, such as
// AWS region dirs (dev/us-east-1 vs prod/us-east-1). Returns "" for an empty
// path so callers can fall back to the directory slug.
func projectNameFromCwd(cwd string) string {
	var clean []string
	for _, p := range strings.Split(cwd, "/") {
		if p != "" {
			clean = append(clean, p)
		}
	}
	switch len(clean) {
	case 0:
		return ""
	case 1:
		return clean[0]
	default:
		return clean[len(clean)-2] + "/" + clean[len(clean)-1]
	}
}

// projectCwdCache memoizes the slug -> cwd lookup so each project's .jsonl
// files are line-scanned at most once per process. A project's recorded cwd is
// stable for the life of the process, so recompute cycles can reuse it instead
// of reopening and scanning the session files every time. The mutex guards
// against concurrent computeStatsCmd goroutines racing on the map.
var (
	projectCwdMu     sync.Mutex
	projectCwdBySlug = map[string]string{}
)

// cachedProjectCwd returns the cwd for a project slug, scanning its session
// files on the first request and serving the memoized result thereafter. The
// empty-string result ("" = no cwd recorded) is itself cached so projects
// without a cwd are not re-scanned on every recompute.
func cachedProjectCwd(slug string, files []string) string {
	projectCwdMu.Lock()
	if cwd, ok := projectCwdBySlug[slug]; ok {
		projectCwdMu.Unlock()
		return cwd
	}
	projectCwdMu.Unlock()

	// Scan outside the lock -- file I/O can be slow and need not be serialized.
	cwd := readProjectCwd(files)

	projectCwdMu.Lock()
	projectCwdBySlug[slug] = cwd
	projectCwdMu.Unlock()
	return cwd
}

// readProjectCwd returns the working directory recorded in a project's session
// files, or "" if none record one. Sessions in a project all share the same
// cwd, so the first file that carries one is sufficient.
func readProjectCwd(files []string) string {
	for _, f := range files {
		if cwd := cwdFromFile(f); cwd != "" {
			return cwd
		}
	}
	return ""
}

// cwdFromFile scans a single .jsonl session file for the first record carrying
// a "cwd" field. The opening record is often metadata without a cwd, so it
// reads subsequent lines until one is found.
func cwdFromFile(path string) string {
	fh, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var rec struct {
			Cwd string `json:"cwd"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		if rec.Cwd != "" {
			return rec.Cwd
		}
	}
	return ""
}
