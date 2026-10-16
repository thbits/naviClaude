package claude

import (
	"encoding/json"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
	"os"
	"path/filepath"
	"testing"
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
		{
			// Bare --resume opens Claude's interactive picker; the following
			// token is another flag, not a session ID, and must not be grabbed.
			name:    "bare --resume followed by a flag",
			cmdLine: "claude --resume --fork-session",
			want:    "",
		},
		{
			name:    "--resume followed by --model flag",
			cmdLine: "claude --resume --model opus",
			want:    "",
		},
		{
			name:    "short -r space-separated form",
			cmdLine: "claude -r abc-123-def",
			want:    "abc-123-def",
		},
		{
			name:    "short -r inline form",
			cmdLine: "claude -r=abc-123-def",
			want:    "abc-123-def",
		},
		{
			name:    "bare -r followed by a flag",
			cmdLine: "claude -r --fork-session",
			want:    "",
		},
		{
			name:    "short -r at end without value",
			cmdLine: "claude -r",
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

func TestMatchesProcessName(t *testing.T) {
	h := New(Options{ProcessNames: []string{"claude", "claude-code"}})

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
			got := h.MatchProcess(tt.input)
			if got != tt.want {
				t.Errorf("MatchProcess(%q) = %v, want %v", tt.input, got, tt.want)
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
		if sess.Harness != Name {
			t.Errorf("Harness = %q, want %q (closed sessions must be stamped to resolve resume/fork)", sess.Harness, Name)
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
		if sess.Status != session.StatusClosed {
			t.Errorf("Status = %v, want session.StatusClosed", sess.Status)
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
		// Empty file has no CWD, so it should be skipped (returns nil).
		if sess != nil {
			t.Fatal("expected nil for empty file with no CWD")
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

func TestNewDefaults(t *testing.T) {
	h := New(Options{})
	if len(h.processNames) == 0 || h.processNames[0] != "claude" {
		t.Errorf("processNames = %v, want [claude]", h.processNames)
	}
	if h.StartCommand() != "claude" {
		t.Errorf("StartCommand() = %q, want claude", h.StartCommand())
	}
	custom := New(Options{Command: "cc --verbose", ProcessNames: []string{"custom-claude"}})
	if custom.processNames[0] != "custom-claude" || custom.StartCommand() != "cc --verbose" {
		t.Errorf("custom options not applied: %v %q", custom.processNames, custom.StartCommand())
	}
}

// TestDetectVersionTitle covers newer Claude retitling its process to the
// version string: tmux reports "2.1.191" while ps still reports "claude".
func TestDetectVersionTitle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := New(Options{})
	d := harness.NewDetector(nil, harness.NewRegistry(h), 0, 0)
	tree := session.NewProcessTree([]session.Proc{{PID: 56273, PPID: 1, Name: "claude"}})
	if !h.MatchProcess(tree.Name(56273)) {
		t.Fatal("ps basename claude must match")
	}
	if h.MatchProcess("2.1.191") {
		t.Fatal("version title alone must not match; the detector relies on the ps basename")
	}
	s := &session.Session{PID: 56273}
	if got := d.ResolveStatus(h, s, tree); got != session.StatusIdle {
		t.Errorf("ResolveStatus with no signals = %v, want Idle", got)
	}
}
