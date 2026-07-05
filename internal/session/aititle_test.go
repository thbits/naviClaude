package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastAITitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")

	// Two ai-title records; the physically-last one is the current title. The
	// file ends with an untimestamped meta record, as real transcripts do.
	lines := []string{
		`{"type":"user","timestamp":"2026-07-05T08:00:00Z"}`,
		`{"type":"ai-title","aiTitle":"First guess"}`,
		`{"type":"assistant","timestamp":"2026-07-05T09:00:00Z"}`,
		`{"type":"ai-title","aiTitle":"Configure Hindsight memory with local Postgres"}`,
		`{"type":"mode"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := lastAITitle(path)
	want := "Configure Hindsight memory with local Postgres"
	if got != want {
		t.Errorf("lastAITitle = %q, want %q (last ai-title wins)", got, want)
	}
}

func TestLastAITitleEmptyOrMissing(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		if got := lastAITitle(""); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if got := lastAITitle(filepath.Join(t.TempDir(), "nope.jsonl")); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("no ai-title records", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "meta.jsonl")
		os.WriteFile(path, []byte(`{"type":"user","timestamp":"2026-07-05T08:00:00Z"}`+"\n"), 0o644)
		if got := lastAITitle(path); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("malformed line skipped", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.jsonl")
		os.WriteFile(path, []byte("not json\n"+`{"type":"ai-title","aiTitle":"Good"}`+"\n"), 0o644)
		if got := lastAITitle(path); got != "Good" {
			t.Errorf("got %q, want %q", got, "Good")
		}
	})
}
