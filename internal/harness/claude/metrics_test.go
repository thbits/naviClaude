package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContextLimitForModel(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"opus", 1_000_000},
		{"Opus", 1_000_000},
		{"OPUS", 1_000_000},
		{"sonnet", 200_000},
		{"haiku", 200_000},
		{"unknown", 200_000},
		{"", 200_000},
		// Claude 5 family ids, with and without a minor version.
		{"claude-opus-5-5", 1_000_000},
		{"claude-sonnet-5-5", 1_000_000},
		{"claude-haiku-5-5", 1_000_000},
		{"claude-sonnet-5", 1_000_000},
		{"claude-opus-5", 1_000_000},
		{"claude-fable-5-1", 1_000_000},
		{"Claude-Haiku-5-5", 1_000_000},
		// Explicit 1M variants.
		{"sonnet[1m]", 1_000_000},
		{"claude-sonnet-4-5[1m]", 1_000_000},
		{"claude-haiku-4-5[1M]", 1_000_000},
		// Legacy ids keep their old windows.
		{"claude-opus-4-8", 1_000_000},
		{"claude-opus-4-6", 1_000_000},
		{"claude-sonnet-4-5", 200_000},
		{"claude-haiku-4-5", 200_000},
		{"claude-3-5-sonnet-20241022", 200_000},
		{"claude-3-5-haiku-20241022", 200_000},
		{"<synthetic>", 200_000},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := ContextLimitForModel(tt.model)
			if got != tt.want {
				t.Errorf("ContextLimitForModel(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestLoadMetrics(t *testing.T) {
	t.Run("counts messages and tokens", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "test-session.jsonl")

		records := []map[string]interface{}{
			{
				"type":      "system",
				"timestamp": "2025-03-01T10:00:00Z",
				"cwd":       "/home/user/project",
			},
			{
				"type":      "user",
				"timestamp": "2025-03-01T10:01:00Z",
				"message":   map[string]interface{}{"content": "hello"},
			},
			{
				"type":      "assistant",
				"timestamp": "2025-03-01T10:02:00Z",
				"message": map[string]interface{}{
					"content": "hi there",
					"usage": map[string]interface{}{
						"input_tokens":  100,
						"output_tokens": 50,
					},
				},
			},
			{
				"type":      "user",
				"timestamp": "2025-03-01T10:03:00Z",
				"message":   map[string]interface{}{"content": "tell me more"},
			},
			{
				"type":      "assistant",
				"timestamp": "2025-03-01T10:04:00Z",
				"message": map[string]interface{}{
					"content": "sure thing",
					"usage": map[string]interface{}{
						"input_tokens":  200,
						"output_tokens": 75,
					},
				},
			},
		}

		writeJSONL(t, filePath, records)

		m, err := LoadMetrics(filePath, "opus")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if m.MessageCount != 4 {
			t.Errorf("MessageCount = %d, want 4", m.MessageCount)
		}

		// TokensUsed reflects the context fill (input + output + cache) from the
		// assistant record with the MAX timestamp, not cumulative. Here the
		// records are in chronological file order, so the latest is the
		// 10:04 record: 200 + 75 = 275.
		wantTokens := 200 + 75
		if m.TokensUsed != wantTokens {
			t.Errorf("TokensUsed = %d, want %d", m.TokensUsed, wantTokens)
		}

		wantStart, _ := time.Parse(time.RFC3339, "2025-03-01T10:00:00Z")
		if !m.StartTime.Equal(wantStart) {
			t.Errorf("StartTime = %v, want %v", m.StartTime, wantStart)
		}

		if m.ContextLimit != 1_000_000 {
			t.Errorf("ContextLimit = %d, want 1000000", m.ContextLimit)
		}
	})

	t.Run("start time is first non-zero timestamp", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "test-start.jsonl")

		records := []map[string]interface{}{
			{
				"type": "system",
				// no timestamp
			},
			{
				"type":      "user",
				"timestamp": "2025-06-15T08:30:00Z",
				"message":   map[string]interface{}{"content": "first"},
			},
			{
				"type":      "user",
				"timestamp": "2025-06-15T08:31:00Z",
				"message":   map[string]interface{}{"content": "second"},
			},
		}

		writeJSONL(t, filePath, records)

		m, err := LoadMetrics(filePath, "sonnet")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantStart, _ := time.Parse(time.RFC3339, "2025-06-15T08:30:00Z")
		if !m.StartTime.Equal(wantStart) {
			t.Errorf("StartTime = %v, want %v", m.StartTime, wantStart)
		}

		if m.ContextLimit != 200_000 {
			t.Errorf("ContextLimit = %d, want 200000", m.ContextLimit)
		}
	})

	t.Run("counts messages spread over time", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "test-activity.jsonl")

		base := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

		records := []map[string]interface{}{
			{
				"type":      "user",
				"timestamp": base.Format(time.RFC3339Nano),
				"message":   map[string]interface{}{"content": "start"},
			},
			{
				"type":      "user",
				"timestamp": base.Add(10 * time.Minute).Format(time.RFC3339Nano),
				"message":   map[string]interface{}{"content": "mid1"},
			},
			{
				"type":      "assistant",
				"timestamp": base.Add(10 * time.Minute).Format(time.RFC3339Nano),
				"message":   map[string]interface{}{"content": "reply", "usage": map[string]interface{}{"input_tokens": 10, "output_tokens": 5}},
			},
			{
				"type":      "user",
				"timestamp": base.Add(47 * time.Minute).Format(time.RFC3339Nano),
				"message":   map[string]interface{}{"content": "end"},
			},
		}

		writeJSONL(t, filePath, records)

		m, err := LoadMetrics(filePath, "haiku")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if m.MessageCount != 4 {
			t.Errorf("MessageCount = %d, want 4", m.MessageCount)
		}

	})

	t.Run("counts only conversational turns", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "turns.jsonl")

		textBlock := func(s string) []interface{} {
			return []interface{}{map[string]interface{}{"type": "text", "text": s}}
		}

		records := []map[string]interface{}{
			// Real human prompt (string content) -> counts.
			{"type": "user", "timestamp": "2025-03-01T10:00:00Z", "message": map[string]interface{}{"content": "hello"}},
			// Assistant thinking-only -> not a message.
			{"type": "assistant", "timestamp": "2025-03-01T10:00:01Z", "message": map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "thinking", "thinking": "hmm"}}}},
			// Assistant visible reply -> counts.
			{"type": "assistant", "timestamp": "2025-03-01T10:00:02Z", "message": map[string]interface{}{"content": textBlock("on it")}},
			// Assistant tool call -> plumbing, not a message.
			{"type": "assistant", "timestamp": "2025-03-01T10:00:03Z", "message": map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "tool_use", "name": "Read"}}}},
			// Tool result -> plumbing, not a message.
			{"type": "user", "timestamp": "2025-03-01T10:00:04Z", "message": map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "tool_result", "content": "file data"}}}},
			// Assistant visible reply -> counts.
			{"type": "assistant", "timestamp": "2025-03-01T10:00:05Z", "message": map[string]interface{}{"content": textBlock("done")}},
			// Real human prompt (text block form) -> counts.
			{"type": "user", "timestamp": "2025-03-01T10:00:06Z", "message": map[string]interface{}{"content": textBlock("thanks")}},
			// Injected context -> not a message.
			{"type": "user", "timestamp": "2025-03-01T10:00:07Z", "isMeta": true, "message": map[string]interface{}{"content": textBlock("<system-reminder>")}},
			// Subagent traffic -> not a message.
			{"type": "assistant", "timestamp": "2025-03-01T10:00:08Z", "isSidechain": true, "message": map[string]interface{}{"content": textBlock("subagent reply")}},
		}

		writeJSONL(t, filePath, records)

		m, err := LoadMetrics(filePath, "opus")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Counts: user "hello", assistant "on it", assistant "done", user "thanks".
		if m.MessageCount != 4 {
			t.Errorf("MessageCount = %d, want 4", m.MessageCount)
		}
	})

	t.Run("non-conversation record with string content is not counted", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "system-string.jsonl")

		records := []map[string]interface{}{
			{"type": "user", "timestamp": "2025-03-01T10:00:00Z", "message": map[string]interface{}{"content": "hello"}},
			// A non-user/assistant record carrying plain-string content must not
			// count as a turn (the type guard precedes the string fast path).
			{"type": "system", "timestamp": "2025-03-01T10:00:01Z", "message": map[string]interface{}{"content": "hook output"}},
		}
		writeJSONL(t, filePath, records)

		m, err := LoadMetrics(filePath, "opus")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.MessageCount != 1 {
			t.Errorf("MessageCount = %d, want 1 (system record excluded)", m.MessageCount)
		}
	})

	t.Run("oversized line does not stop the scan", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "oversized.jsonl")

		// A line well past any fixed buffer, then the record carrying the model.
		valid := `{"type":"user","timestamp":"2025-03-01T10:00:00Z","message":{"content":"before the big line"}}` + "\n"
		huge := `{"type":"user","message":{"content":"` + strings.Repeat("x", 6*1024*1024) + `"}}` + "\n"
		after := `{"type":"assistant","timestamp":"2025-03-01T10:00:02Z","message":{"model":"claude-sonnet-5-5","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":150000}}}` + "\n"
		if err := os.WriteFile(filePath, []byte(valid+huge+after), 0o644); err != nil {
			t.Fatal(err)
		}

		m, err := LoadMetrics(filePath, "sonnet")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.MessageCount != 3 {
			t.Errorf("MessageCount = %d, want 3", m.MessageCount)
		}
		if m.ModelID != "claude-sonnet-5-5" || m.ContextLimit != 1_000_000 || m.TokensUsed != 150_000 {
			t.Errorf("got model %q limit %d tokens %d, want claude-sonnet-5-5 / 1M / 150K read past the huge line",
				m.ModelID, m.ContextLimit, m.TokensUsed)
		}
	})

	t.Run("final line without newline", func(t *testing.T) {
		filePath := filepath.Join(t.TempDir(), "nonl.jsonl")
		rec := `{"type":"assistant","timestamp":"2025-03-01T10:00:02Z","message":{"model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":10}}}`
		if err := os.WriteFile(filePath, []byte(rec), 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := LoadMetrics(filePath, "opus")
		if err != nil || m.MessageCount != 1 || m.ModelID != "claude-opus-5-5" {
			t.Errorf("got %+v err %v, want the unterminated record parsed", m, err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "empty.jsonl")
		os.WriteFile(filePath, []byte(""), 0o644)

		m, err := LoadMetrics(filePath, "sonnet")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.MessageCount != 0 {
			t.Errorf("MessageCount = %d, want 0", m.MessageCount)
		}
		if m.TokensUsed != 0 {
			t.Errorf("TokensUsed = %d, want 0", m.TokensUsed)
		}
		if !m.StartTime.IsZero() {
			t.Errorf("StartTime should be zero, got %v", m.StartTime)
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		_, err := LoadMetrics("/nonexistent/file.jsonl", "sonnet")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("malformed JSON lines skipped", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "malformed.jsonl")
		content := "not json\n" +
			`{"type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"content":"hi"}}` + "\n" +
			"also bad\n"
		os.WriteFile(filePath, []byte(content), 0o644)

		m, err := LoadMetrics(filePath, "sonnet")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.MessageCount != 1 {
			t.Errorf("MessageCount = %d, want 1", m.MessageCount)
		}
	})

	t.Run("assistant without usage", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "no-usage.jsonl")

		records := []map[string]interface{}{
			{
				"type":      "assistant",
				"timestamp": "2025-03-01T10:00:00Z",
				"message":   map[string]interface{}{"content": "hello"},
			},
		}
		writeJSONL(t, filePath, records)

		m, err := LoadMetrics(filePath, "sonnet")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.MessageCount != 1 {
			t.Errorf("MessageCount = %d, want 1", m.MessageCount)
		}
		if m.TokensUsed != 0 {
			t.Errorf("TokensUsed = %d, want 0", m.TokensUsed)
		}
	})

	t.Run("single message", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "single.jsonl")

		records := []map[string]interface{}{
			{
				"type":      "user",
				"timestamp": "2025-01-01T12:00:00Z",
				"message":   map[string]interface{}{"content": "only message"},
			},
		}
		writeJSONL(t, filePath, records)

		m, err := LoadMetrics(filePath, "sonnet")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.MessageCount != 1 {
			t.Errorf("MessageCount = %d, want 1", m.MessageCount)
		}
	})
}

// writeJSONL is a test helper that writes a slice of records as JSONL.
func TestResolveContextLimit(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		fallback string
		tokens   int
		want     int
	}{
		{"id wins over family", "claude-haiku-5-5", "haiku", 62_000, 1_000_000},
		{"fallback when no id", "", "sonnet", 62_000, 200_000},
		{"fallback opus", "", "opus", 0, 1_000_000},
		{"legacy within limit", "claude-sonnet-4-5", "sonnet", 199_999, 200_000},
		{"legacy at limit", "claude-sonnet-4-5", "sonnet", 200_000, 200_000},
		{"over 200K bumps to 1M", "claude-sonnet-4-5", "sonnet", 200_001, 1_000_000},
		{"unknown over 200K bumps", "", "", 250_000, 1_000_000},
		{"beyond largest tier stays largest", "claude-opus-5-5", "opus", 1_200_000, 1_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveContextLimit(tt.modelID, tt.fallback, tt.tokens)
			if got != tt.want {
				t.Errorf("resolveContextLimit(%q, %q, %d) = %d, want %d",
					tt.modelID, tt.fallback, tt.tokens, got, tt.want)
			}
		})
	}
}

func TestLoadMetricsContextLimitFromTranscript(t *testing.T) {
	assistant := func(ts, model string, input int) map[string]interface{} {
		return map[string]interface{}{
			"type":      "assistant",
			"timestamp": ts,
			"message": map[string]interface{}{
				"model":   model,
				"content": "ok",
				"usage":   map[string]interface{}{"input_tokens": input},
			},
		}
	}

	tests := []struct {
		name       string
		records    []map[string]interface{}
		family     string
		wantLimit  int
		wantTokens int
		wantModel  string
	}{
		{
			// Classified family "haiku" alone would yield 200K.
			name:       "haiku 5.5 id overrides family",
			records:    []map[string]interface{}{assistant("2026-10-16T22:03:49Z", "claude-haiku-5-5", 62_718)},
			family:     "haiku",
			wantLimit:  1_000_000,
			wantTokens: 62_718,
			wantModel:  "claude-haiku-5-5",
		},
		{
			name: "latest record's model is used",
			records: []map[string]interface{}{
				assistant("2026-10-16T10:00:00Z", "claude-sonnet-5-5", 1_000),
				assistant("2026-10-16T11:00:00Z", "claude-sonnet-4-5", 2_000),
			},
			family:     "sonnet",
			wantLimit:  200_000,
			wantTokens: 2_000,
			wantModel:  "claude-sonnet-4-5",
		},
		{
			name: "synthetic record keeps prior model",
			records: []map[string]interface{}{
				assistant("2026-10-16T10:00:00Z", "claude-haiku-5-5", 1_000),
				assistant("2026-10-16T11:00:00Z", "<synthetic>", 0),
			},
			family:     "haiku",
			wantLimit:  1_000_000,
			wantTokens: 0,
			wantModel:  "claude-haiku-5-5",
		},
		{
			name:       "no model in transcript falls back to family",
			records:    []map[string]interface{}{assistant("2026-10-16T10:00:00Z", "", 5_000)},
			family:     "sonnet",
			wantLimit:  200_000,
			wantTokens: 5_000,
		},
		{
			name:       "usage beyond window bumps tier",
			records:    []map[string]interface{}{assistant("2026-10-16T10:00:00Z", "", 250_000)},
			family:     "sonnet",
			wantLimit:  1_000_000,
			wantTokens: 250_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(t.TempDir(), "s.jsonl")
			writeJSONL(t, filePath, tt.records)
			m, err := LoadMetrics(filePath, tt.family)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.TokensUsed != tt.wantTokens {
				t.Errorf("TokensUsed = %d, want %d", m.TokensUsed, tt.wantTokens)
			}
			if m.ContextLimit != tt.wantLimit {
				t.Errorf("ContextLimit = %d, want %d", m.ContextLimit, tt.wantLimit)
			}
			if m.ModelID != tt.wantModel {
				t.Errorf("ModelID = %q, want %q", m.ModelID, tt.wantModel)
			}
		})
	}
}

func writeJSONL(t *testing.T, filePath string, records []map[string]interface{}) {
	t.Helper()
	var content []byte
	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal record: %v", err)
		}
		content = append(content, line...)
		content = append(content, '\n')
	}
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write JSONL: %v", err)
	}
}
