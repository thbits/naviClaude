package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLastMessageTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")

	t1 := "2026-06-25T08:00:00.000Z"
	t2 := "2026-06-25T09:00:00.000Z"
	t3 := "2026-06-25T10:00:00.000Z" // the maximum

	// Records are written out of order and the file ends with an untimestamped
	// meta record, so a correct implementation must take the MAX timestamp
	// across records, not the last line.
	lines := []string{
		`{"type":"user","timestamp":"` + t1 + `"}`,
		`{"type":"file-history-snapshot"}`, // no timestamp
		`{"type":"assistant","timestamp":"` + t3 + `"}`,
		`{"type":"user","timestamp":"` + t2 + `"}`,
		`{"type":"custom-title"}`, // untimestamped meta as the final line
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := lastMessageTime(path)
	want, _ := time.Parse(time.RFC3339Nano, t3)
	if !got.Equal(want) {
		t.Errorf("lastMessageTime = %v, want %v (max across records)", got, want)
	}
}

func TestLastMessageTimeEmptyOrMissing(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		if got := lastMessageTime(""); !got.IsZero() {
			t.Errorf("got %v, want zero", got)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if got := lastMessageTime(filepath.Join(t.TempDir(), "nope.jsonl")); !got.IsZero() {
			t.Errorf("got %v, want zero", got)
		}
	})
	t.Run("no timestamped records", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "meta.jsonl")
		os.WriteFile(path, []byte("{\"type\":\"custom-title\"}\n{\"type\":\"mode\"}\n"), 0o644)
		if got := lastMessageTime(path); !got.IsZero() {
			t.Errorf("got %v, want zero (no timestamps)", got)
		}
	})
}
