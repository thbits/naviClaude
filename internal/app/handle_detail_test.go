package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

func TestLoadDetailDataCmd(t *testing.T) {
	t.Run("counts only conversational turns", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "detail.jsonl")

		// Two real turns (human prompt + assistant reply) buried in tool
		// plumbing and injected context.
		content := `{"type":"user","timestamp":"2025-03-01T10:00:00Z","message":{"content":"hello"}}
{"type":"assistant","timestamp":"2025-03-01T10:00:01Z","message":{"content":[{"type":"tool_use","name":"Read"}]}}
{"type":"user","timestamp":"2025-03-01T10:00:02Z","message":{"content":[{"type":"tool_result","content":"data"}]}}
{"type":"assistant","timestamp":"2025-03-01T10:00:03Z","message":{"content":[{"type":"text","text":"done"}]}}
{"type":"user","timestamp":"2025-03-01T10:00:04Z","isMeta":true,"message":{"content":[{"type":"text","text":"<system-reminder>"}]}}
`
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		sess := &session.Session{ID: "abc", SessionFile: filePath}
		msg := loadDetailDataCmd(sess)()

		data, ok := msg.(detailDataMsg)
		if !ok {
			t.Fatalf("expected detailDataMsg, got %T", msg)
		}
		if data.messageCount != 2 {
			t.Errorf("messageCount = %d, want 2", data.messageCount)
		}
	})
}
