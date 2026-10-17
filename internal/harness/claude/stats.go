package claude

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/thbits/naviClaude/internal/stats"
)

// Chart spans, in local days, for the week and all-time filters.
const (
	statsWeekDays = 7
	statsAllDays  = 30
)

// statsMaxProjects caps the Top Projects list.
const statsMaxProjects = 8

// Compute builds Stats for filter ("all", "week", "today") from the session
// transcripts under claudeDir/projects.
func Compute(claudeDir string, activeCount int, filter string) (*stats.Stats, error) {
	if claudeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		claudeDir = filepath.Join(home, ".claude")
	}
	return computeStats(defaultTranscriptCache, claudeDir, activeCount, filter, time.Now()), nil
}

func computeStats(cache *transcriptCache, claudeDir string, activeCount int, filter string, now time.Time) *stats.Stats {
	files := listTranscripts(claudeDir)
	sums := cache.summaries(files)
	home, _ := os.UserHomeDir()
	st := aggregate(files, sums, filter, now, home)
	st.ActiveCount = activeCount
	return st
}

// periodStart returns the local-midnight start of the filter's period, or the
// zero time for "all".
func periodStart(filter string, now time.Time) time.Time {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch filter {
	case "today":
		return midnight
	case "week":
		return midnight.AddDate(0, 0, -(statsWeekDays - 1))
	}
	return time.Time{}
}

type projectAgg struct {
	cwd      string
	sessions int
	prompts  int
}

// aggregate folds per-file summaries into the period's Stats. Records copied
// into forked sessions are counted once, by the file whose events start first.
func aggregate(files []transcriptFile, sums map[string]*transcriptSummary, filter string, now time.Time, home string) *stats.Stats {
	now = now.Local()
	start := periodStart(filter, now)
	st := &stats.Stats{Filter: filter, Since: start, BusiestHour: -1}

	var mains, subs []transcriptFile
	for _, f := range files {
		if sums[f.path] == nil {
			continue
		}
		if f.main {
			mains = append(mains, f)
		} else {
			subs = append(subs, f)
		}
	}
	firstTS := func(f transcriptFile) int64 {
		s := sums[f.path]
		ts := int64(1<<63 - 1)
		for _, p := range s.Prompts {
			ts = min(ts, p.TS)
		}
		for _, u := range s.Usage {
			ts = min(ts, u.TS)
		}
		return ts
	}
	sort.SliceStable(mains, func(i, j int) bool { return firstTS(mains[i]) < firstTS(mains[j]) })

	days := statsAllDays
	switch filter {
	case "today":
		days = 1
	case "week":
		days = statsWeekDays
	}
	dayIndex := make(map[string]int, days)
	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, i-(days-1)).Format("2006-01-02")
		dayIndex[d] = i
		st.Daily = append(st.Daily, stats.DayActivity{Date: d})
	}

	var oldest int64
	startUnix := start.Unix()
	inPeriod := func(ts int64) bool {
		if oldest == 0 || ts < oldest {
			oldest = ts
		}
		return start.IsZero() || ts >= startUnix
	}
	seenPrompt := map[uint64]bool{}
	seenUsage := map[uint64]bool{}
	familyTokens := make([]int64, len(modelFamilies))
	addUsage := func(s *transcriptSummary) bool {
		active := false
		for _, u := range s.Usage {
			if seenUsage[u.ID] {
				continue
			}
			seenUsage[u.ID] = true
			if !inPeriod(u.TS) {
				continue
			}
			active = true
			st.Tokens.Input += u.Input
			st.Tokens.Output += u.Output
			st.Tokens.CacheRead += u.CacheRead
			st.Tokens.CacheWrite += u.CacheWrite
			familyTokens[u.Family] += u.Input + u.Output + u.CacheRead + u.CacheWrite
		}
		return active
	}

	projects := map[string]*projectAgg{}
	var allCwds []string
	for _, f := range mains {
		s := sums[f.path]
		cwd := projectCwd(f, s)
		allCwds = append(allCwds, cwd)
		active, prompts := false, 0
		for _, p := range s.Prompts {
			if seenPrompt[p.ID] {
				continue
			}
			seenPrompt[p.ID] = true
			if !inPeriod(p.TS) {
				continue
			}
			active = true
			prompts++
			t := time.Unix(p.TS, 0).In(now.Location())
			st.Hourly[t.Hour()]++
			if i, ok := dayIndex[t.Format("2006-01-02")]; ok {
				st.Daily[i].Prompts++
			}
		}
		if addUsage(s) {
			active = true
		}
		if !active {
			continue
		}
		st.Sessions++
		st.Prompts += prompts
		pa := projects[cwd]
		if pa == nil {
			pa = &projectAgg{cwd: cwd}
			projects[cwd] = pa
		}
		pa.sessions++
		pa.prompts += prompts
	}
	for _, f := range subs {
		addUsage(sums[f.path])
	}

	if start.IsZero() && oldest > 0 {
		st.Since = time.Unix(oldest, 0).In(now.Location())
	}
	st.Projects = rankProjects(projects, projectLabels(allCwds, home))
	for i, n := range familyTokens {
		if n > 0 {
			st.Models = append(st.Models, stats.ModelUsageEntry{Family: modelFamilies[i], Tokens: n})
		}
	}
	sort.SliceStable(st.Models, func(i, j int) bool { return st.Models[i].Tokens > st.Models[j].Tokens })
	for h, n := range st.Hourly {
		if n > 0 && (st.BusiestHour < 0 || n > st.Hourly[st.BusiestHour]) {
			st.BusiestHour = h
		}
	}
	return st
}

// projectCwd is the directory a session was started in; later records drift
// as the shell cds. Falls back to the project slug when no record has a cwd.
func projectCwd(f transcriptFile, s *transcriptSummary) string {
	if s.Cwd != "" {
		return s.Cwd
	}
	return filepath.Base(filepath.Dir(f.path))
}

// rankProjects orders projects by sessions then prompts. labels come from every
// known project so a label means the same directory under every filter.
func rankProjects(projects map[string]*projectAgg, labels map[string]string) []stats.ProjectCount {
	out := make([]stats.ProjectCount, 0, len(projects))
	for cwd, pa := range projects {
		out = append(out, stats.ProjectCount{Name: labels[cwd], Sessions: pa.sessions, Prompts: pa.prompts})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sessions != out[j].Sessions {
			return out[i].Sessions > out[j].Sessions
		}
		if out[i].Prompts != out[j].Prompts {
			return out[i].Prompts > out[j].Prompts
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > statsMaxProjects {
		out = out[:statsMaxProjects]
	}
	return out
}

// projectLabels gives each cwd the shortest trailing-path label no other cwd shares,
// rooted at "~" under home; other absolute paths shown in full keep their "/".
func projectLabels(cwds []string, home string) map[string]string {
	segs := make(map[string][]string, len(cwds))
	depth := make(map[string]int, len(cwds))
	var uniq []string
	for _, cwd := range cwds {
		if _, ok := segs[cwd]; !ok {
			segs[cwd] = pathSegments(cwd, home)
			depth[cwd] = 1
			uniq = append(uniq, cwd)
		}
	}
	cwds = uniq
	label := func(cwd string) string {
		s := segs[cwd]
		k := min(depth[cwd], len(s))
		l := strings.Join(s[len(s)-k:], "/")
		if k == len(s) && strings.HasPrefix(cwd, "/") && s[0] != "~" {
			l = "/" + l
		}
		return l
	}
	for {
		groups := map[string][]string{}
		for _, cwd := range cwds {
			groups[label(cwd)] = append(groups[label(cwd)], cwd)
		}
		grew := false
		for _, g := range groups {
			if len(g) < 2 {
				continue
			}
			for _, cwd := range g {
				if depth[cwd] < len(segs[cwd]) {
					depth[cwd]++
					grew = true
				}
			}
		}
		if !grew {
			break
		}
	}
	out := make(map[string]string, len(cwds))
	for _, cwd := range cwds {
		out[cwd] = label(cwd)
	}
	return out
}

func pathSegments(cwd, home string) []string {
	var segs []string
	rest := cwd
	if home != "" && (cwd == home || strings.HasPrefix(cwd, home+"/")) {
		segs = []string{"~"}
		rest = strings.TrimPrefix(cwd, home)
	}
	for _, p := range strings.Split(rest, "/") {
		if p != "" {
			segs = append(segs, p)
		}
	}
	if len(segs) == 0 {
		return []string{cwd}
	}
	return segs
}
