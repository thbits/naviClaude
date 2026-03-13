package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyModel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude-opus-4-6", "opus"},
		{"claude-sonnet-4-6", "sonnet"},
		{"claude-haiku-4-5-20251001", "haiku"},
		{"Claude-OPUS-4-6", "opus"},
		{"SONNET", "sonnet"},
		{"gpt-4", "gpt-4"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := classifyModel(tt.input)
			if got != tt.want {
				t.Errorf("classifyModel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseResumeFlag(t *testing.T) {
	tests := []struct {
		name    string
		cmdLine string
		want    string
	}{
		{
			name:    "resume flag present",
			cmdLine: "claude --resume abc-123-def",
			want:    "abc-123-def",
		},
		{
			name:    "resume flag with other args",
			cmdLine: "/usr/bin/claude --verbose --resume my-session-id --json",
			want:    "my-session-id",
		},
		{
			name:    "no resume flag",
			cmdLine: "claude --json",
			want:    "",
		},
		{
			name:    "resume at end without value",
			cmdLine: "claude --resume",
			want:    "",
		},
		{
			name:    "empty command line",
			cmdLine: "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResumeFlag(tt.cmdLine)
			if got != tt.want {
				t.Errorf("parseResumeFlag(%q) = %q, want %q", tt.cmdLine, got, tt.want)
			}
		})
	}
}

func TestIsWaitingForInput(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "confirmation prompt Y/n",
			content: "Do you want to proceed? [Y/n]",
			want:    true,
		},
		{
			name:    "confirmation prompt y/N",
			content: "Continue? [y/N]",
			want:    true,
		},
		{
			name:    "allow prompt",
			content: "Allow?",
			want:    true,
		},
		{
			name:    "enter to select",
			content: "Use arrow keys to navigate\nEnter to select",
			want:    true,
		},
		{
			name:    "enter to confirm",
			content: "press Enter to confirm",
			want:    true,
		},
		{
			name:    "interrupted session",
			content: "What should Claude do instead?",
			want:    true,
		},
		{
			name:    "normal output not waiting",
			content: "Building project...\nCompiling main.go\nDone.",
			want:    false,
		},
		{
			name:    "empty content",
			content: "",
			want:    false,
		},
		{
			name:    "case insensitive y/n",
			content: "Overwrite? [y/n]",
			want:    true,
		},
		{
			name:    "n/y variant",
			content: "Delete files? [n/y]",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWaitingForInput(tt.content)
			if got != tt.want {
				t.Errorf("isWaitingForInput() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLastNNonEmptyLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		n     int
		want  int
	}{
		{
			name:  "basic lines",
			input: "line1\nline2\nline3",
			n:     2,
			want:  2,
		},
		{
			name:  "empty lines skipped",
			input: "line1\n\n\nline2\n\n",
			n:     5,
			want:  2,
		},
		{
			name:  "empty string",
			input: "",
			n:     3,
			want:  0,
		},
		{
			name:  "request more than available",
			input: "only",
			n:     10,
			want:  1,
		},
		{
			name:  "with ANSI codes stripped",
			input: "\x1b[31mred\x1b[0m\n\x1b[32mgreen\x1b[0m",
			n:     2,
			want:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lastNNonEmptyLines(tt.input, tt.n)
			if len(got) != tt.want {
				t.Errorf("got %d lines, want %d", len(got), tt.want)
			}
		})
	}
}

func TestStripANSIBasic(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no ANSI",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "color code",
			input: "\x1b[31mred text\x1b[0m",
			want:  "red text",
		},
		{
			name:  "multiple sequences",
			input: "\x1b[1m\x1b[32mbold green\x1b[0m normal",
			want:  "bold green normal",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "bare escape without bracket",
			input: "hello\x1bXworld",
			want:  "helloworld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSIBasic(tt.input)
			if got != tt.want {
				t.Errorf("stripANSIBasic(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestProcessTreeStats(t *testing.T) {
	tree := &ProcessTree{
		children: make(map[int][]int),
		names:    make(map[int]string),
		ppid:     make(map[int]int),
		cpu:      map[int]float64{100: 12.5},
		rss:      map[int]float64{100: 51200}, // 50 MB in KB
	}

	cpu, mem := tree.Stats(100)
	if cpu != 12.5 {
		t.Errorf("cpu = %f, want 12.5", cpu)
	}
	if mem != 50.0 {
		t.Errorf("mem = %f, want 50.0", mem)
	}

	// Unknown PID returns zeros.
	cpu, mem = tree.Stats(999)
	if cpu != 0 || mem != 0 {
		t.Errorf("unknown PID: cpu=%f, mem=%f, want 0, 0", cpu, mem)
	}
}

func TestProcessTreeIsAncestorOf(t *testing.T) {
	tree := &ProcessTree{
		children: make(map[int][]int),
		names:    make(map[int]string),
		ppid: map[int]int{
			10: 1,
			20: 10,
			30: 20,
			40: 30,
		},
		cpu: make(map[int]float64),
		rss: make(map[int]float64),
	}

	tests := []struct {
		name      string
		ancestor  int
		desc      int
		want      bool
	}{
		{"direct parent", 20, 30, true},
		{"grandparent", 10, 30, true},
		{"great-grandparent", 10, 40, true},
		{"not ancestor", 30, 10, false},
		{"same PID", 20, 20, false},
		{"unknown PID", 999, 30, false},
		{"root process", 1, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tree.isAncestorOf(tt.ancestor, tt.desc)
			if got != tt.want {
				t.Errorf("isAncestorOf(%d, %d) = %v, want %v", tt.ancestor, tt.desc, got, tt.want)
			}
		})
	}
}

func TestProcessTreeFindMatchingDescendant(t *testing.T) {
	tree := &ProcessTree{
		children: map[int][]int{
			1:  {10},
			10: {20, 30},
			20: {40},
		},
		names: map[int]string{
			10: "bash",
			20: "node",
			30: "claude",
			40: "python",
		},
		ppid: make(map[int]int),
		cpu:  make(map[int]float64),
		rss:  make(map[int]float64),
	}

	matchClaude := func(name string) bool { return name == "claude" }

	t.Run("finds direct child", func(t *testing.T) {
		got := tree.findMatchingDescendant(10, matchClaude, 3)
		if got != 30 {
			t.Errorf("got PID %d, want 30", got)
		}
	})

	t.Run("no match returns 0", func(t *testing.T) {
		matchVim := func(name string) bool { return name == "vim" }
		got := tree.findMatchingDescendant(1, matchVim, 10)
		if got != 0 {
			t.Errorf("got PID %d, want 0", got)
		}
	})

	t.Run("maxDepth 0 returns 0", func(t *testing.T) {
		got := tree.findMatchingDescendant(10, matchClaude, 0)
		if got != 0 {
			t.Errorf("got PID %d, want 0", got)
		}
	})

	t.Run("depth limited search", func(t *testing.T) {
		// claude is at depth 1 from PID 10, so maxDepth=1 should find it.
		got := tree.findMatchingDescendant(10, matchClaude, 1)
		if got != 30 {
			t.Errorf("got PID %d, want 30", got)
		}
	})
}

func TestNewDetector(t *testing.T) {
	t.Run("defaults process names", func(t *testing.T) {
		d := NewDetector(nil, nil, 0)
		if len(d.processNames) == 0 {
			t.Fatal("expected default process names")
		}
		if d.processNames[0] != "claude" {
			t.Errorf("processNames[0] = %q, want %q", d.processNames[0], "claude")
		}
	})

	t.Run("defaults active window", func(t *testing.T) {
		d := NewDetector(nil, nil, 0)
		if d.activeWindow != 5*time.Second {
			t.Errorf("activeWindow = %v, want 5s", d.activeWindow)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		d := NewDetector(nil, []string{"custom-claude"}, 10)
		if d.processNames[0] != "custom-claude" {
			t.Errorf("processNames[0] = %q, want %q", d.processNames[0], "custom-claude")
		}
		if d.activeWindow != 10*time.Second {
			t.Errorf("activeWindow = %v, want 10s", d.activeWindow)
		}
	})

	t.Run("negative active window uses default", func(t *testing.T) {
		d := NewDetector(nil, nil, -1)
		if d.activeWindow != 5*time.Second {
			t.Errorf("activeWindow = %v, want 5s", d.activeWindow)
		}
	})
}

func TestMatchesProcessName(t *testing.T) {
	d := NewDetector(nil, []string{"claude", "claude-code"}, 5)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"exact match", "claude", true},
		{"case insensitive", "Claude", true},
		{"absolute path", "/usr/local/bin/claude", true},
		{"second name", "claude-code", true},
		{"no match", "vim", false},
		{"partial match not counted", "claude-helper", false},
		{"empty name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.matchesProcessName(tt.input)
			if got != tt.want {
				t.Errorf("matchesProcessName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSessionStatus(t *testing.T) {
	tests := []struct {
		status SessionStatus
		want   string
	}{
		{StatusActive, "Active"},
		{StatusIdle, "Idle"},
		{StatusWaiting, "Waiting"},
		{StatusClosed, "Closed"},
		{SessionStatus(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestParseSessionFile(t *testing.T) {
	t.Run("valid session file", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "abc-123.jsonl")

		records := []map[string]interface{}{
			{
				"type":      "system",
				"timestamp": "2025-03-01T10:00:00Z",
				"cwd":       "/home/user/project",
				"gitBranch": "main",
				"version":   "2.1.73",
				"slug":      "happy-coding-session",
			},
			{
				"type":      "assistant",
				"timestamp": "2025-03-01T10:01:00Z",
				"message":   map[string]string{"model": "claude-opus-4-6", "role": "assistant"},
			},
		}

		var content []byte
		for _, rec := range records {
			line, _ := json.Marshal(rec)
			content = append(content, line...)
			content = append(content, '\n')
		}
		os.WriteFile(filePath, content, 0o644)

		sess, err := parseSessionFile(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sess == nil {
			t.Fatal("expected session, got nil")
		}
		if sess.ID != "abc-123" {
			t.Errorf("ID = %q, want %q", sess.ID, "abc-123")
		}
		if sess.CWD != "/home/user/project" {
			t.Errorf("CWD = %q, want %q", sess.CWD, "/home/user/project")
		}
		if sess.GitBranch != "main" {
			t.Errorf("GitBranch = %q, want %q", sess.GitBranch, "main")
		}
		if sess.Model != "opus" {
			t.Errorf("Model = %q, want %q", sess.Model, "opus")
		}
		if sess.Slug != "happy-coding-session" {
			t.Errorf("Slug = %q, want %q", sess.Slug, "happy-coding-session")
		}
		if sess.Status != StatusClosed {
			t.Errorf("Status = %v, want StatusClosed", sess.Status)
		}
		if sess.ProjectName != "project" {
			t.Errorf("ProjectName = %q, want %q", sess.ProjectName, "project")
		}
	})

	t.Run("empty file returns nil", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "empty.jsonl")
		os.WriteFile(filePath, []byte(""), 0o644)

		sess, err := parseSessionFile(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Empty file still gets an ID from filename, so sess is non-nil.
		// The session will have ID "empty" from the filename.
		if sess == nil {
			t.Fatal("expected session with ID from filename")
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		_, err := parseSessionFile("/nonexistent/file.jsonl")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("malformed JSON lines skipped", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "test-sess.jsonl")
		content := "not json\n{\"type\":\"system\",\"cwd\":\"/tmp\"}\nalso not json\n"
		os.WriteFile(filePath, []byte(content), 0o644)

		sess, err := parseSessionFile(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sess == nil {
			t.Fatal("expected session")
		}
		if sess.CWD != "/tmp" {
			t.Errorf("CWD = %q, want %q", sess.CWD, "/tmp")
		}
	})
}

func TestHistoryScanner(t *testing.T) {
	t.Run("LoadHistoryIndex with valid file", func(t *testing.T) {
		dir := t.TempDir()
		entries := []HistoryEntry{
			{Display: "first prompt", SessionID: "sess-1", Project: "proj1", Timestamp: 1000},
			{Display: "second prompt", SessionID: "sess-2", Project: "proj2", Timestamp: 2000},
			{Display: "later prompt", SessionID: "sess-1", Project: "proj1", Timestamp: 3000},
		}

		var content []byte
		for _, e := range entries {
			line, _ := json.Marshal(e)
			content = append(content, line...)
			content = append(content, '\n')
		}
		os.WriteFile(filepath.Join(dir, "history.jsonl"), content, 0o644)

		hs, err := NewHistoryScanner(dir)
		if err != nil {
			t.Fatalf("NewHistoryScanner error: %v", err)
		}

		index, err := hs.LoadHistoryIndex()
		if err != nil {
			t.Fatalf("LoadHistoryIndex error: %v", err)
		}

		if len(index) != 2 {
			t.Fatalf("got %d entries, want 2", len(index))
		}
		if index["sess-1"] != "first prompt" {
			t.Errorf("sess-1 display = %q, want %q", index["sess-1"], "first prompt")
		}
		if index["sess-2"] != "second prompt" {
			t.Errorf("sess-2 display = %q, want %q", index["sess-2"], "second prompt")
		}
	})

	t.Run("LoadHistoryIndex with missing file returns empty map", func(t *testing.T) {
		dir := t.TempDir()
		hs, err := NewHistoryScanner(dir)
		if err != nil {
			t.Fatalf("NewHistoryScanner error: %v", err)
		}

		index, err := hs.LoadHistoryIndex()
		if err != nil {
			t.Fatalf("LoadHistoryIndex error: %v", err)
		}
		if len(index) != 0 {
			t.Errorf("expected empty index, got %d entries", len(index))
		}
	})

	t.Run("caching returns same result", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(`{"display":"test","sessionId":"s1"}`+"\n"), 0o644)

		hs, _ := NewHistoryScanner(dir)
		idx1, _ := hs.LoadHistoryIndex()
		idx2, _ := hs.LoadHistoryIndex()

		if len(idx1) != len(idx2) {
			t.Error("cached result should match")
		}
	})
}
