package session

import (
	"encoding/json"
	"os"
	"path/filepath"
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

		// TokensUsed reflects the LAST assistant record's context fill
		// (input + output + cache), not cumulative. Last record: 200 + 75 = 275.
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

	t.Run("recent activity bucketing", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "test-activity.jsonl")

		// Adaptive bucketing: 10 equal slots across session lifetime.
		// Session spans 50 minutes (from t+0 to t+47m).
		// Each bucket covers ~5 minutes.
		base := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

		records := []map[string]interface{}{
			{
				"type":      "user",
				"timestamp": base.Format(time.RFC3339Nano), // bucket 0
				"message":   map[string]interface{}{"content": "start"},
			},
			{
				"type":      "user",
				"timestamp": base.Add(10 * time.Minute).Format(time.RFC3339Nano), // ~bucket 2
				"message":   map[string]interface{}{"content": "mid1"},
			},
			{
				"type":      "assistant",
				"timestamp": base.Add(10 * time.Minute).Format(time.RFC3339Nano), // ~bucket 2
				"message":   map[string]interface{}{"content": "reply", "usage": map[string]interface{}{"input_tokens": 10, "output_tokens": 5}},
			},
			{
				"type":      "user",
				"timestamp": base.Add(47 * time.Minute).Format(time.RFC3339Nano), // bucket 9 (last)
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

		// All 4 messages should land in some bucket.
		total := 0
		for _, v := range m.RecentActivity {
			total += v
		}
		if total != 4 {
			t.Errorf("total RecentActivity = %d, want 4", total)
		}

		// First message should be in bucket 0.
		if m.RecentActivity[0] != 1 {
			t.Errorf("RecentActivity[0] = %d, want 1", m.RecentActivity[0])
		}

		// Last message should be in bucket 9.
		if m.RecentActivity[9] != 1 {
			t.Errorf("RecentActivity[9] = %d, want 1", m.RecentActivity[9])
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

	t.Run("single message goes to last bucket", func(t *testing.T) {
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
		// Single message should go to bucket 9.
		if m.RecentActivity[9] != 1 {
			t.Errorf("RecentActivity[9] = %d, want 1", m.RecentActivity[9])
		}
	})
}

// writeJSONL is a test helper that writes a slice of records as JSONL.
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
