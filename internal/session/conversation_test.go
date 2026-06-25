package session

import (
	"path/filepath"
	"testing"
)

// TestLoadConversationReturnsLastTurns verifies that when a session has more
// than maxTurns text-bearing turns, LoadConversation returns the LAST maxTurns
// in chronological order (oldest-to-newest within the tail).
func TestLoadConversationReturnsLastTurns(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "convo.jsonl")

	records := []map[string]interface{}{
		{"type": "user", "message": map[string]interface{}{"content": "turn-1"}},
		{"type": "assistant", "message": map[string]interface{}{"content": "turn-2"}},
		{"type": "user", "message": map[string]interface{}{"content": "turn-3"}},
		{"type": "assistant", "message": map[string]interface{}{"content": "turn-4"}},
		{"type": "user", "message": map[string]interface{}{"content": "turn-5"}},
	}
	writeJSONL(t, filePath, records)

	sess := &Session{ID: "convo", SessionFile: filePath}

	entries, err := LoadConversation(sess, 2)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	// Should be the LAST two turns, in chronological order.
	if entries[0].Text != "turn-4" {
		t.Errorf("entries[0].Text = %q, want %q", entries[0].Text, "turn-4")
	}
	if entries[1].Text != "turn-5" {
		t.Errorf("entries[1].Text = %q, want %q", entries[1].Text, "turn-5")
	}
}

// TestLoadConversationNonTextTurnsSkipped verifies tool-use / empty-text turns
// do not count toward the tail and the LAST text turns still surface.
func TestLoadConversationNonTextTurnsSkipped(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "convo-skip.jsonl")

	records := []map[string]interface{}{
		{"type": "user", "message": map[string]interface{}{"content": "a"}},
		// tool-only assistant turn yields no text and must be skipped.
		{"type": "assistant", "message": map[string]interface{}{
			"content": []map[string]interface{}{{"type": "tool_use", "name": "x"}},
		}},
		{"type": "user", "message": map[string]interface{}{"content": "b"}},
		{"type": "assistant", "message": map[string]interface{}{"content": "c"}},
	}
	writeJSONL(t, filePath, records)

	sess := &Session{ID: "convo-skip", SessionFile: filePath}

	entries, err := LoadConversation(sess, 2)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Text != "b" || entries[1].Text != "c" {
		t.Errorf("entries = [%q, %q], want [b, c]", entries[0].Text, entries[1].Text)
	}
}

// TestLoadConversationFewerThanMax returns all turns when there are fewer than
// maxTurns, preserving order.
func TestLoadConversationFewerThanMax(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "convo-few.jsonl")

	records := []map[string]interface{}{
		{"type": "user", "message": map[string]interface{}{"content": "one"}},
		{"type": "assistant", "message": map[string]interface{}{"content": "two"}},
	}
	writeJSONL(t, filePath, records)

	sess := &Session{ID: "convo-few", SessionFile: filePath}

	entries, err := LoadConversation(sess, 10)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Text != "one" || entries[1].Text != "two" {
		t.Errorf("entries = [%q, %q], want [one, two]", entries[0].Text, entries[1].Text)
	}
}
