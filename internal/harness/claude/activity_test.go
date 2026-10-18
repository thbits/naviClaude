package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var actBase = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func ats(sec int) string {
	return actBase.Add(time.Duration(sec) * time.Second).Format(time.RFC3339Nano)
}

func jl(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func prompt(sec int, text string) string {
	return jl(map[string]any{"type": "user", "timestamp": ats(sec), "message": map[string]any{"role": "user", "content": text}})
}

func metaUser(sec int, text string) string {
	return jl(map[string]any{"type": "user", "isMeta": true, "timestamp": ats(sec), "message": map[string]any{"role": "user", "content": text}})
}

func assistantText(sec int, text string) string {
	return jl(map[string]any{"type": "assistant", "timestamp": ats(sec), "message": map[string]any{
		"model": "claude-opus-5-5", "content": []any{map[string]any{"type": "text", "text": text}},
		"usage": map[string]any{"input_tokens": 10, "cache_read_input_tokens": 61000, "output_tokens": 990},
	}})
}

func toolUse(sec int, id, name string, input map[string]any) string {
	return jl(map[string]any{"type": "assistant", "timestamp": ats(sec), "message": map[string]any{
		"model": "claude-opus-5-5", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}},
	}})
}

func toolResult(sec int, id string, async bool) string {
	rec := map[string]any{"type": "user", "timestamp": ats(sec), "message": map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok"},
	}}}
	if async {
		rec["toolUseResult"] = map[string]any{"isAsync": true, "status": "async_launched"}
	}
	return jl(rec)
}

func turnEnd(sec, pendingBg int) string {
	return jl(map[string]any{"type": "system", "subtype": "turn_duration", "timestamp": ats(sec), "pendingBackgroundAgentCount": pendingBg})
}

func notification(sec int, id string) string {
	return prompt(sec, "<task-notification>\n<task-id>a1</task-id>\n<tool-use-id>"+id+"</tool-use-id>\n<status>completed</status>\n</task-notification>")
}

// queuedNotification is how a notification that lands mid-turn is recorded.
func queuedNotification(sec int, id string) string {
	return jl(map[string]any{"type": "attachment", "timestamp": ats(sec), "attachment": map[string]any{
		"type": "queued_command", "prompt": "<task-notification>\n<tool-use-id>" + id + "</tool-use-id>\n</task-notification>",
	}})
}

func bgShellResult(sec int, id, task string) string {
	return jl(map[string]any{"type": "user", "timestamp": ats(sec), "toolUseResult": map[string]any{"backgroundTaskId": task},
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok"}}}})
}

func shellNotification(sec int, id string) string {
	return prompt(sec, "<task-notification>\n<task-id>b1</task-id>\n<tool-use-id>"+id+"</tool-use-id>\n<status>completed</status>\n<summary>Background command \"x\" completed</summary>\n</task-notification>")
}

func TestLoadActivityShells(t *testing.T) {
	bash := func(sec int, id string) string {
		return toolUse(sec, id, "Bash", map[string]any{"command": "make watch", "run_in_background": true})
	}
	tests := []struct {
		name           string
		lines          []string
		shells, agents int
	}{
		{"running shells count", []string{prompt(0, "go"), bash(1, "s1"), bgShellResult(1, "s1", "b1"),
			bash(2, "s2"), bgShellResult(2, "s2", "b2"), assistantText(3, "ok"), turnEnd(4, 0)}, 2, 0},
		{"notification clears a shell", []string{prompt(0, "go"), bash(1, "s1"), bgShellResult(1, "s1", "b1"),
			assistantText(3, "ok"), turnEnd(4, 0), shellNotification(9, "s1")}, 0, 0},
		{"TaskStop clears a shell", []string{prompt(0, "go"), bash(1, "s1"), bgShellResult(1, "s1", "b1"),
			toolUse(2, "k1", "TaskStop", map[string]any{"task_id": "b1"})}, 0, 0},
		{"shell notification leaves the agent floor alone", []string{prompt(0, "go"), assistantText(1, "ok"), turnEnd(2, 1),
			shellNotification(5, "gone")}, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadActivity(writeTranscript(t, tt.lines...))
			if err != nil {
				t.Fatal(err)
			}
			if got.Shells != tt.shells || got.Agents != tt.agents {
				t.Errorf("shells/agents = %d/%d, want %d/%d", got.Shells, got.Agents, tt.shells, tt.agents)
			}
		})
	}
}

func TestLoadActivityDropsPriorRun(t *testing.T) {
	bash := func(sec int, id string) string {
		return toolUse(sec, id, "Bash", map[string]any{"command": "make watch", "run_in_background": true})
	}
	agent := func(sec int, id string) string {
		return toolUse(sec, id, "Agent", map[string]any{"description": "dig", "run_in_background": true})
	}
	prior := []string{prompt(0, "go"), bash(1, "s1"), bgShellResult(1, "s1", "b1"),
		agent(2, "a1"), toolResult(2, "a1", true), assistantText(3, "ok"), turnEnd(4, 1)}
	tests := []struct {
		name           string
		lines          []string
		since          int // seconds after actBase the process started; -1 = unknown
		shells, agents int
	}{
		{"unknown start keeps everything", prior, -1, 1, 1},
		{"run still live keeps everything", prior, 0, 1, 1},
		{"resumed with no new records", prior, 60, 0, 0},
		{"resumed run counts only its own shells", append(append([]string{}, prior...),
			prompt(61, "again"), bash(62, "s2"), bgShellResult(62, "s2", "b2")), 60, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var since time.Time
			if tt.since >= 0 {
				since = actBase.Add(time.Duration(tt.since) * time.Second)
			}
			got, err := loadActivity(writeTranscript(t, tt.lines...), since)
			if err != nil {
				t.Fatal(err)
			}
			if got.Shells != tt.shells || got.Agents != tt.agents {
				t.Errorf("shells/agents = %d/%d, want %d/%d", got.Shells, got.Agents, tt.shells, tt.agents)
			}
		})
	}
}

func TestLoadActivityCacheKeysOnStart(t *testing.T) {
	path := writeTranscript(t, prompt(0, "go"), toolUse(1, "s1", "Bash", map[string]any{"command": "make watch", "run_in_background": true}),
		bgShellResult(1, "s1", "b1"))
	if got, _ := loadActivity(path, actBase); got.Shells != 1 {
		t.Fatalf("live run: shells = %d, want 1", got.Shells)
	}
	if got, _ := loadActivity(path, actBase.Add(time.Minute)); got.Shells != 0 {
		t.Errorf("after restart the cached count leaked: shells = %d, want 0", got.Shells)
	}
}

func sidechain(sec int, id, name string) string {
	return jl(map[string]any{"type": "assistant", "isSidechain": true, "timestamp": ats(sec), "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{"command": "ls"}}},
	}})
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadActivity(t *testing.T) {
	tests := []struct {
		name       string
		lines      []string
		tool       string
		target     string
		turnStart  int // seconds after actBase; -1 = zero
		lastAsst   int // -1 = zero
		agents     int
		tokensUsed int
		ended      bool
	}{
		{
			name: "working bash prefers short command",
			lines: []string{prompt(0, "run the tests"), assistantText(2, "ok"),
				toolUse(3, "t1", "Bash", map[string]any{"command": "go test ./...", "description": "Run tests"})},
			tool: "Bash", target: "go test ./...", turnStart: 0, lastAsst: 3, tokensUsed: 62000,
		},
		{
			name: "long bash command falls back to description",
			lines: []string{prompt(0, "go"),
				toolUse(1, "t1", "Bash", map[string]any{"command": strings.Repeat("x", 80), "description": "Build everything"})},
			tool: "Bash", target: "Build everything", turnStart: 0, lastAsst: 1,
		},
		{
			name:  "edit shows base file name",
			lines: []string{prompt(5, "fix it"), toolUse(6, "t1", "Edit", map[string]any{"file_path": "/repo/internal/ui/sidebar.go"})},
			tool:  "Edit", target: "sidebar.go", turnStart: 5, lastAsst: 6,
		},
		{
			name: "thinking: tool finished, none in flight",
			lines: []string{prompt(0, "go"), toolUse(1, "t1", "Read", map[string]any{"file_path": "/a/b.go"}),
				toolResult(2, "t1", false)},
			turnStart: 0, lastAsst: 1,
		},
		{
			name:  "waiting approval keeps latest unanswered tool",
			lines: []string{prompt(0, "go"), toolUse(1, "t1", "Write", map[string]any{"file_path": "/x/main.go"})},
			tool:  "Write", target: "main.go", turnStart: 0, lastAsst: 1,
		},
		{
			name: "question",
			lines: []string{prompt(0, "plan it"), toolUse(4, "q1", "AskUserQuestion", map[string]any{
				"questions": []any{map[string]any{"question": "Which approach?\nmore"}}})},
			tool: "AskUserQuestion", target: "Which approach?", turnStart: 0, lastAsst: 4,
		},
		{
			name:  "plan approval",
			lines: []string{prompt(0, "plan"), toolUse(9, "p1", "ExitPlanMode", map[string]any{"plan": "..."})},
			tool:  "ExitPlanMode", turnStart: 0, lastAsst: 9,
		},
		{
			name:      "done: turn ended",
			lines:     []string{prompt(0, "hi"), assistantText(3, "hello"), turnEnd(4, 0)},
			turnStart: 0, lastAsst: 3, tokensUsed: 62000, ended: true,
		},
		{
			name: "foreground agent in flight",
			lines: []string{prompt(0, "go"), toolUse(1, "a1", "Agent", map[string]any{"description": "Fix wrong context"}),
				toolUse(2, "a2", "Task", map[string]any{"description": "Other"})},
			tool: "Task", target: "Other", turnStart: 0, lastAsst: 2, agents: 2,
		},
		{
			name: "background agents count until notified",
			lines: []string{prompt(0, "go"),
				toolUse(1, "a1", "Agent", map[string]any{"description": "one"}), toolResult(1, "a1", true),
				toolUse(2, "a2", "Agent", map[string]any{"description": "two"}), toolResult(2, "a2", true),
				assistantText(3, "launched"), turnEnd(4, 2), notification(60, "a1"), assistantText(61, "a1 done"), turnEnd(62, 1)},
			turnStart: 60, lastAsst: 61, agents: 1, tokensUsed: 62000, ended: true,
		},
		{
			name: "notification mid-turn does not restart the turn",
			lines: []string{prompt(0, "go"), toolUse(1, "a1", "Agent", map[string]any{"description": "bg"}), toolResult(1, "a1", true),
				toolUse(5, "t1", "Bash", map[string]any{"command": "make"}), notification(8, "a1")},
			tool: "Bash", target: "make", turnStart: 0, lastAsst: 5,
		},
		{
			name: "queued notification clears a finished background agent",
			lines: []string{prompt(0, "go"), toolUse(1, "a1", "Agent", map[string]any{"description": "bg"}), toolResult(1, "a1", true),
				toolUse(5, "t1", "Bash", map[string]any{"command": "make"}), toolResult(6, "t1", false), queuedNotification(8, "a1"),
				assistantText(9, "done"), turnEnd(10, 0),
				prompt(20, "next"), toolUse(21, "a2", "Agent", map[string]any{"description": "fg"})},
			tool: "Agent", target: "fg", turnStart: 20, lastAsst: 21, agents: 1, tokensUsed: 62000,
		},
		{
			name: "sidechain records are ignored",
			lines: []string{prompt(0, "go"), toolUse(1, "a1", "Agent", map[string]any{"description": "sub"}),
				sidechain(2, "s1", "Bash")},
			tool: "Agent", target: "sub", turnStart: 0, lastAsst: 1, agents: 1,
		},
		{
			name: "interrupt clears in-flight tools",
			lines: []string{prompt(0, "go"), toolUse(1, "t1", "Bash", map[string]any{"command": "sleep 100"}),
				prompt(5, "[Request interrupted by user for tool use]")},
			turnStart: 0, lastAsst: 1, ended: true,
		},
		{
			name: "mcp tool uses short name",
			lines: []string{prompt(0, "search"),
				toolUse(1, "m1", "mcp__claude_ai_Notion__notion-search", map[string]any{"query": "x"})},
			tool: "MCP", target: "notion-search", turnStart: 0, lastAsst: 1,
		},
		{
			name:  "webfetch shows host; local command output is not a prompt",
			lines: []string{prompt(0, "fetch"), toolUse(1, "w1", "WebFetch", map[string]any{"url": "https://pkg.go.dev/x?y=1"}), prompt(2, "<local-command-stdout>hi</local-command-stdout>")},
			tool:  "WebFetch", target: "pkg.go.dev", turnStart: 0, lastAsst: 1,
		},
		{
			name: "unanswered notification does not hold the turn open",
			lines: []string{prompt(0, "hi"), assistantText(1, "yo"), turnEnd(2, 1), notification(3, "a1"),
				metaUser(300, "Another Claude session sent a message"), toolUse(301, "t1", "Read", map[string]any{"file_path": "/x/y.md"})},
			tool: "Read", target: "y.md", turnStart: 300, lastAsst: 301, tokensUsed: 62000,
		},
		{
			name: "turn end without a count clears the background floor",
			lines: []string{prompt(0, "go"), assistantText(1, "launched"), turnEnd(2, 1),
				prompt(10, "next"), assistantText(11, "ok"), jl(map[string]any{"type": "system", "subtype": "turn_duration", "timestamp": ats(12)})},
			turnStart: 10, lastAsst: 11, tokensUsed: 62000, ended: true,
		},
		{
			name:      "meta message starts a turn only when idle",
			lines:     []string{prompt(0, "hi"), assistantText(1, "yo"), turnEnd(2, 0), metaUser(30, "Another Claude session sent a message")},
			turnStart: 30, lastAsst: 1, tokensUsed: 62000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadActivity(writeTranscript(t, tt.lines...))
			if err != nil {
				t.Fatal(err)
			}
			if got.Tool != tt.tool || got.Target != tt.target {
				t.Errorf("tool = %q/%q, want %q/%q", got.Tool, got.Target, tt.tool, tt.target)
			}
			if want := actBase.Add(time.Duration(tt.turnStart) * time.Second); !got.TurnStart.Equal(want) {
				t.Errorf("TurnStart = %v, want %v", got.TurnStart, want)
			}
			if got.TurnStartApprox {
				t.Error("TurnStartApprox set for a fully-read file")
			}
			if want := actBase.Add(time.Duration(tt.lastAsst) * time.Second); !got.LastAssistant.Equal(want) {
				t.Errorf("LastAssistant = %v, want %v", got.LastAssistant, want)
			}
			if got.TurnEnded != tt.ended {
				t.Errorf("TurnEnded = %v, want %v", got.TurnEnded, tt.ended)
			}
			if got.Agents != tt.agents {
				t.Errorf("Agents = %d, want %d", got.Agents, tt.agents)
			}
			if got.TokensUsed != tt.tokensUsed {
				t.Errorf("TokensUsed = %d, want %d", got.TokensUsed, tt.tokensUsed)
			}
		})
	}
}

// A huge transcript must be read only from its tail: the head here is an
// unterminated garbage blob that would break any full-file parse.
func TestLoadActivityReadsOnlyTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.jsonl")
	head := prompt(0, "old prompt") + "\n" + strings.Repeat("x", 8<<20) + "\n"
	var tail strings.Builder
	tail.WriteString(prompt(100, "current prompt") + "\n")
	tail.WriteString(toolUse(101, "t1", "Grep", map[string]any{"pattern": "renderSparkline"}) + "\n")
	if err := os.WriteFile(path, []byte(head+tail.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tool != "Grep" || got.Target != "renderSparkline" {
		t.Errorf("tool = %q/%q, want Grep/renderSparkline", got.Tool, got.Target)
	}
	if want := actBase.Add(100 * time.Second); !got.TurnStart.Equal(want) || got.TurnStartApprox {
		t.Errorf("TurnStart = %v approx=%v, want %v exact", got.TurnStart, got.TurnStartApprox, want)
	}

	data, truncated, err := readActivityTail(path, activityTailBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(data) > activityTailBytes {
		t.Errorf("readActivityTail returned %d bytes (truncated=%v), want <= %d", len(data), truncated, activityTailBytes)
	}
}

// When the turn began before the tail window, TurnStart is a flagged lower
// bound, and later polls keep that earlier bound as the window slides.
func TestLoadActivityApproxTurnStartIsSticky(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.jsonl")
	var b strings.Builder
	b.WriteString(prompt(0, "start") + "\n")
	pad := strings.Repeat("y", 1000)
	sec := 1
	for b.Len() < activityTailMaxBytes+activityTailBytes {
		b.WriteString(toolUse(sec, "r"+ats(sec), "Read", map[string]any{"file_path": "/f.go", "pad": pad}) + "\n")
		b.WriteString(toolResult(sec, "r"+ats(sec), false) + "\n")
		sec++
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := LoadActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.TurnStartApprox || first.TurnStart.Equal(actBase) || first.TurnStart.IsZero() {
		t.Fatalf("first read: TurnStart = %v approx=%v, want an approximate lower bound", first.TurnStart, first.TurnStartApprox)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		sec++
		f.WriteString(toolUse(sec, "z"+ats(sec), "Read", map[string]any{"file_path": "/f.go", "pad": pad}) + "\n")
	}
	f.Close()
	future := time.Now().Add(time.Minute)
	os.Chtimes(path, future, future)

	second, err := LoadActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	if !second.TurnStart.Equal(first.TurnStart) || !second.TurnStartApprox {
		t.Errorf("second read: TurnStart = %v approx=%v, want sticky %v", second.TurnStart, second.TurnStartApprox, first.TurnStart)
	}
}

func TestLoadActivityMissingFile(t *testing.T) {
	if _, err := LoadActivity(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Error("want an error for a missing transcript")
	}
}

func TestLoadActivityRecordsMainThreadModel(t *testing.T) {
	assistantModel := func(sec int, model string, sidechain bool) string {
		return jl(map[string]any{"type": "assistant", "isSidechain": sidechain, "timestamp": ats(sec), "message": map[string]any{
			"model": model, "content": []any{map[string]any{"type": "text", "text": "x"}},
		}})
	}
	path := writeTranscript(t,
		prompt(0, "go"),
		assistantModel(1, "claude-opus-5-5", false),
		assistantModel(2, "claude-sonnet-5-5", false),
		assistantModel(3, "claude-haiku-5-5", true), // subagent traffic
		assistantModel(4, "<synthetic>", false),     // local error record
	)
	act, err := LoadActivity(path)
	if err != nil {
		t.Fatal(err)
	}
	if act.Model != "claude-sonnet-5-5" {
		t.Errorf("Model = %q, want the latest main-thread model claude-sonnet-5-5", act.Model)
	}
}
