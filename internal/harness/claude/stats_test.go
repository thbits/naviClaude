package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/stats"
)

// statsFixture writes JSONL transcripts under a temp claude dir.
type statsFixture struct {
	t   *testing.T
	dir string
}

func newStatsFixture(t *testing.T) *statsFixture {
	return &statsFixture{t: t, dir: t.TempDir()}
}

func (fx *statsFixture) write(rel string, recs ...map[string]any) string {
	fx.t.Helper()
	p := filepath.Join(fx.dir, "projects", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		fx.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(statsJSONL(fx.t, recs...)), 0o644); err != nil {
		fx.t.Fatal(err)
	}
	return p
}

func (fx *statsFixture) compute(filter string, now time.Time) *stats.Stats {
	return computeStats(&transcriptCache{}, fx.dir, 0, filter, now)
}

func statsJSONL(t *testing.T, recs ...map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, r := range recs {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

func statsTS(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func statsPrompt(uuid string, at time.Time, text string) map[string]any {
	return map[string]any{
		"type": "user", "uuid": uuid, "timestamp": statsTS(at), "cwd": "/work/proj",
		"isSidechain": false, "origin": map[string]any{"kind": "human"},
		"message": map[string]any{"role": "user", "content": text},
	}
}

func statsReply(uuid, msgID, req string, at time.Time, model string, in, out, cr, cw int) map[string]any {
	return map[string]any{
		"type": "assistant", "uuid": uuid, "timestamp": statsTS(at), "cwd": "/work/proj", "requestId": req,
		"message": map[string]any{
			"id": msgID, "model": model, "role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": cr, "cache_creation_input_tokens": cw},
		},
	}
}

var statsNow = time.Date(2026, 10, 17, 15, 30, 0, 0, time.Local)

func TestComputeCountsPromptsAndTokens(t *testing.T) {
	fx := newStatsFixture(t)
	at := statsNow.Add(-time.Hour)
	toolResult := map[string]any{
		"type": "user", "uuid": "u-tool", "timestamp": statsTS(at), "toolUseResult": map[string]any{"stdout": "x"},
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": "x"}}},
	}
	meta := statsPrompt("u-meta", at, "skill body")
	meta["isMeta"] = true
	compact := statsPrompt("u-compact", at, "This session is being continued")
	compact["isCompactSummary"] = true
	notification := statsPrompt("u-notify", at, "<task-notification>done</task-notification>")
	notification["origin"] = map[string]any{"kind": "task-notification"}
	legacyCmd := statsPrompt("u-cmd", at, "<command-name>/clear</command-name>")
	delete(legacyCmd, "origin")
	legacyPrompt := statsPrompt("u-legacy", at, "an older-format prompt")
	delete(legacyPrompt, "origin")
	interrupted := map[string]any{
		"type": "user", "uuid": "u-int", "timestamp": statsTS(at),
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "[Request interrupted by user]"}}},
	}
	synthetic := statsReply("a-syn", "msg_syn", "req_syn", at, "<synthetic>", 0, 0, 0, 0)

	fx.write("-work-proj/s1.jsonl",
		map[string]any{"type": "mode", "sessionId": "s1"},
		statsPrompt("u1", at, "fix the bug"),
		// One API response is written as one record per content block.
		statsReply("a1", "msg_1", "req_1", at, "claude-opus-5-5", 10, 100, 1000, 50),
		statsReply("a2", "msg_1", "req_1", at, "claude-opus-5-5", 10, 100, 1000, 50),
		toolResult, meta, compact, notification, legacyCmd, legacyPrompt, interrupted, synthetic,
		statsReply("a3", "msg_2", "req_2", at, "claude-sonnet-5", 1, 20, 0, 0),
	)
	sub := statsReply("s-a1", "msg_sub", "req_sub", at, "claude-haiku-5-5", 5, 7, 0, 0)
	sub["isSidechain"] = true
	subPrompt := statsPrompt("s-u1", at, "subagent instructions")
	subPrompt["isSidechain"] = true
	fx.write("-work-proj/s1/subagents/agent-x.jsonl", subPrompt, sub)

	st := fx.compute("all", statsNow)
	if st.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1 (subagent transcripts are not sessions)", st.Sessions)
	}
	if st.Prompts != 2 {
		t.Errorf("Prompts = %d, want 2 (typed prompt + legacy prompt)", st.Prompts)
	}
	want := stats.Tokens{Input: 16, Output: 127, CacheRead: 1000, CacheWrite: 50}
	if st.Tokens != want {
		t.Errorf("Tokens = %+v, want %+v (msg_1 once, sonnet, subagent haiku)", st.Tokens, want)
	}
	gotModels := map[string]int64{}
	for _, m := range st.Models {
		gotModels[m.Family] = m.Tokens
	}
	if gotModels["opus"] != 1160 || gotModels["sonnet"] != 21 || gotModels["haiku"] != 12 || len(gotModels) != 3 {
		t.Errorf("Models = %+v", st.Models)
	}
	if st.Models[0].Family != "opus" {
		t.Errorf("Models not sorted by tokens: %+v", st.Models)
	}
}

func TestComputeDedupesForkedSessions(t *testing.T) {
	fx := newStatsFixture(t)
	day := statsNow.Add(-2 * time.Hour)
	shared := []map[string]any{
		statsPrompt("u1", day, "first"),
		statsReply("a1", "msg_1", "req_1", day, "claude-opus-5-5", 1, 10, 0, 0),
	}
	fx.write("-work-proj/orig.jsonl", shared...)
	// A pure copy contributes nothing new; a fork that continues adds its own turn.
	fx.write("-work-proj/copy.jsonl", shared...)
	fx.write("-work-proj/fork.jsonl", append(shared,
		statsPrompt("u2", day.Add(time.Minute), "second"),
		statsReply("a2", "msg_2", "req_2", day.Add(time.Minute), "claude-opus-5-5", 1, 5, 0, 0))...)

	st := fx.compute("all", statsNow)
	if st.Prompts != 2 || st.Tokens.Output != 15 {
		t.Errorf("Prompts = %d Output = %d, want 2 and 15", st.Prompts, st.Tokens.Output)
	}
	if st.Sessions != 2 {
		t.Errorf("Sessions = %d, want 2 (orig and its pure copy share every record)", st.Sessions)
	}
}

func TestComputeFilters(t *testing.T) {
	fx := newStatsFixture(t)
	today := time.Date(2026, 10, 17, 9, 0, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1).Add(5 * time.Hour)
	sixDaysAgo := time.Date(2026, 10, 11, 0, 0, 1, 0, time.Local)
	sevenDaysAgo := time.Date(2026, 10, 10, 23, 59, 0, 0, time.Local)
	fx.write("-a/today.jsonl", statsPrompt("t1", today, "a"), statsPrompt("t2", today.Add(time.Minute), "b"),
		statsReply("ta", "m1", "r1", today, "claude-opus-5-5", 0, 10, 0, 0))
	fx.write("-a/yesterday.jsonl", statsPrompt("y1", yesterday, "a"))
	fx.write("-a/six.jsonl", statsPrompt("s1", sixDaysAgo, "a"))
	fx.write("-a/seven.jsonl", statsPrompt("v1", sevenDaysAgo, "a"))

	tests := []struct {
		filter            string
		sessions, prompts int
		days              int
		since             time.Time
	}{
		{"today", 1, 2, 1, time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)},
		{"week", 3, 4, 7, time.Date(2026, 10, 11, 0, 0, 0, 0, time.Local)},
		{"all", 4, 5, 30, sevenDaysAgo.Truncate(time.Second)},
	}
	for _, tt := range tests {
		t.Run(tt.filter, func(t *testing.T) {
			st := fx.compute(tt.filter, statsNow)
			if st.Sessions != tt.sessions || st.Prompts != tt.prompts {
				t.Errorf("Sessions = %d Prompts = %d, want %d and %d", st.Sessions, st.Prompts, tt.sessions, tt.prompts)
			}
			if len(st.Daily) != tt.days || st.Daily[len(st.Daily)-1].Date != "2026-10-17" {
				t.Fatalf("Daily = %+v, want %d days ending today", st.Daily, tt.days)
			}
			if !st.Since.Equal(tt.since) {
				t.Errorf("Since = %v, want %v", st.Since, tt.since)
			}
			sum := 0
			for _, d := range st.Daily {
				sum += d.Prompts
			}
			if sum != tt.prompts {
				t.Errorf("daily buckets sum to %d, want %d", sum, tt.prompts)
			}
			if st.BusiestHour != 9 || st.Hourly[9] != 2 {
				t.Errorf("BusiestHour = %d (Hourly[9] = %d), want 9 with 2", st.BusiestHour, st.Hourly[9])
			}
		})
	}
}

func TestComputeNoActivity(t *testing.T) {
	fx := newStatsFixture(t)
	fx.write("-a/old.jsonl", statsPrompt("o1", statsNow.AddDate(0, 0, -3), "a"))
	st := fx.compute("today", statsNow)
	if st.Sessions != 0 || st.Prompts != 0 || st.BusiestHour != -1 || len(st.Projects) != 0 {
		t.Errorf("got %+v, want an empty period with BusiestHour -1", st)
	}
}

func TestProjectLabels(t *testing.T) {
	home := "/Users/me"
	tests := []struct {
		name string
		cwds []string
		want map[string]string
	}{
		{"home is tilde", []string{"/Users/me"}, map[string]string{"/Users/me": "~"}},
		{"unique last segment", []string{"/Users/me/git/naviClaude", "/Users/me/git/opmed-tf"},
			map[string]string{"/Users/me/git/naviClaude": "naviClaude", "/Users/me/git/opmed-tf": "opmed-tf"}},
		{"shared suffix grows until distinct", []string{
			"/Users/me/git/claude-marketplace/skills/devops-pr-review",
			"/Users/me/.agents/skills/devops-pr-review",
			"/Users/me/git/other",
		}, map[string]string{
			"/Users/me/git/claude-marketplace/skills/devops-pr-review": "claude-marketplace/skills/devops-pr-review",
			"/Users/me/.agents/skills/devops-pr-review":                ".agents/skills/devops-pr-review",
			"/Users/me/git/other":                                      "other",
		}},
		{"region dirs", []string{"/Users/me/tf/dev/us-east-1", "/Users/me/tf/prod/us-east-1"},
			map[string]string{"/Users/me/tf/dev/us-east-1": "dev/us-east-1", "/Users/me/tf/prod/us-east-1": "prod/us-east-1"}},
		{"suffix of another path", []string{"/Users/me/x", "/Users/me/a/x", "/x"},
			map[string]string{"/Users/me/x": "~/x", "/Users/me/a/x": "a/x", "/x": "/x"}},
		{"duplicates collapse", []string{"/Users/me/p", "/Users/me/p"}, map[string]string{"/Users/me/p": "p"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectLabels(tt.cwds, home)
			for cwd, want := range tt.want {
				if got[cwd] != want {
					t.Errorf("label(%q) = %q, want %q", cwd, got[cwd], want)
				}
			}
		})
	}
}

func TestComputeProjectsUseSessionStartDir(t *testing.T) {
	fx := newStatsFixture(t)
	at := statsNow.Add(-time.Hour)
	p := statsPrompt("u1", at, "a")
	p["cwd"] = "/work/one/app"
	drift := statsPrompt("u2", at, "b")
	drift["cwd"] = "/work/one/app/sub" // the shell cd'd; the session still belongs to app
	fx.write("-work-one-app/s1.jsonl", p, drift)
	q := statsPrompt("u3", at, "c")
	q["cwd"] = "/work/two/app"
	fx.write("-work-two-app/s2.jsonl", q)

	st := fx.compute("all", statsNow)
	names := map[string]int{}
	for _, pc := range st.Projects {
		names[pc.Name] = pc.Prompts
	}
	if names["one/app"] != 2 || names["two/app"] != 1 || len(names) != 2 {
		t.Errorf("Projects = %+v, want one/app (2 prompts) and two/app (1)", st.Projects)
	}
}

func TestTranscriptCacheInvalidation(t *testing.T) {
	fx := newStatsFixture(t)
	at := statsNow.Add(-time.Hour)
	path := fx.write("-a/s.jsonl", statsPrompt("u1", at, "a"))
	cachePath := filepath.Join(t.TempDir(), "cache.gob")
	c := &transcriptCache{path: cachePath}
	run := func(c *transcriptCache) *stats.Stats { return computeStats(c, fx.dir, 0, "all", statsNow) }

	if got := run(c).Prompts; got != 1 {
		t.Fatalf("Prompts = %d, want 1", got)
	}

	// Same size and mtime: the cached summary is served without reading the file.
	info, _ := os.Stat(path)
	same := statsJSONL(t, statsPrompt("u9", at.Add(time.Hour), "b"))
	if len(same) != int(info.Size()) {
		t.Fatalf("statsFixture sizes differ: %d vs %d", len(same), info.Size())
	}
	if err := os.WriteFile(path, []byte(same), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := run(c).BusiestHour; got != at.Hour() {
		t.Fatalf("unchanged (size, mtime) rescanned: BusiestHour = %d, want %d", got, at.Hour())
	}
	// The on-disk cache serves a fresh process the same way.
	if got := run(&transcriptCache{path: cachePath}).BusiestHour; got != at.Hour() {
		t.Fatalf("disk cache not reused: BusiestHour = %d, want %d", got, at.Hour())
	}

	// Appending is picked up, parsing from the previous offset.
	if err := os.WriteFile(path, []byte(statsJSONL(t, statsPrompt("u1", at, "a"))), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(statsJSONL(t, statsPrompt("u2", at, "c"), statsPrompt("u3", at, "d")))
	f.Close()
	if got := run(c).Prompts; got != 3 {
		t.Errorf("after append Prompts = %d, want 3", got)
	}

	// A rewrite that shrinks the file forces a full rescan.
	if err := os.WriteFile(path, []byte(statsJSONL(t, statsPrompt("u7", at, "e"))), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := run(c).Prompts; got != 1 {
		t.Errorf("after rewrite Prompts = %d, want 1", got)
	}

	// Deleted transcripts drop out.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := run(c); got.Prompts != 0 || len(c.files) != 0 {
		t.Errorf("after delete Prompts = %d cached files = %d, want 0 and 0", got.Prompts, len(c.files))
	}
}

func TestTranscriptCacheDiscardsOtherVersion(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "cache.gob")
	c := &transcriptCache{path: cachePath, files: map[string]*transcriptSummary{"/x": {Size: 1}}}
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	if got := (&transcriptCache{path: cachePath}).load(); len(got) != 1 {
		t.Fatalf("round-trip lost entries: %v", got)
	}
	raw, _ := os.ReadFile(cachePath)
	_ = os.WriteFile(cachePath, raw[:len(raw)/2], 0o644)
	if got := (&transcriptCache{path: cachePath}).load(); len(got) != 0 {
		t.Errorf("torn cache file decoded into %d entries", len(got))
	}
}

func TestScanTranscriptLongAndPartialLines(t *testing.T) {
	fx := newStatsFixture(t)
	at := statsNow.Add(-time.Hour)
	huge := map[string]any{"type": "attachment", "content": strings.Repeat("x", 3<<20)}
	path := fx.write("-a/s.jsonl", huge, statsPrompt("u1", at, "after a 3MB line"))

	// A record still being written has no newline yet and must wait for it.
	partial := statsJSONL(t, statsPrompt("u2", at, "in flight"))
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(partial[:len(partial)/2])
	f.Close()

	info, _ := os.Stat(path)
	s, err := scanTranscript(path, true, info, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Prompts) != 1 {
		t.Fatalf("Prompts = %d, want 1 (long line skipped, partial line deferred)", len(s.Prompts))
	}
	if s.Offset != info.Size()-int64(len(partial)/2) {
		t.Errorf("Offset = %d, want end of last complete line %d", s.Offset, info.Size()-int64(len(partial)/2))
	}

	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(partial[len(partial)/2:])
	f.Close()
	info, _ = os.Stat(path)
	s, err = scanTranscript(path, true, info, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Prompts) != 2 || s.Offset != info.Size() {
		t.Errorf("after completing the line Prompts = %d Offset = %d, want 2 and %d", len(s.Prompts), s.Offset, info.Size())
	}
}

func TestListTranscriptsSkipsToolResults(t *testing.T) {
	fx := newStatsFixture(t)
	fx.write("-a/one.jsonl", statsPrompt("u1", statsNow, "a"))
	fx.write("-b/two.jsonl", statsPrompt("u2", statsNow, "a"))
	fx.write("-b/two/subagents/agent-1.jsonl", statsPrompt("u3", statsNow, "a"))
	fx.write("-b/two/tool-results/out.jsonl", statsPrompt("u4", statsNow, "a"))
	if got := listTranscripts(fx.dir); len(got) != 3 {
		t.Errorf("listTranscripts found %d files, want 3 (tool-results ignored)", len(got))
	}
}

func TestComputeConcurrentCallsShareCache(t *testing.T) {
	fx := newStatsFixture(t)
	for i := 0; i < 20; i++ {
		fx.write(filepath.Join("-a", string(rune('a'+i))+".jsonl"), statsPrompt(string(rune('a'+i)), statsNow.Add(-time.Hour), "x"))
	}
	c := &transcriptCache{path: filepath.Join(t.TempDir(), "cache.gob")}
	done := make(chan int)
	for i := 0; i < 4; i++ {
		go func() { done <- computeStats(c, fx.dir, 0, "all", statsNow).Prompts }()
	}
	for i := 0; i < 4; i++ {
		if got := <-done; got != 20 {
			t.Errorf("Prompts = %d, want 20", got)
		}
	}
}
