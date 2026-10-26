package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeAITitleJSONL writes a transcript with a single ai-title record under a
// fake ~/.claude/projects/<slug>/ tree. HOME must already be a temp dir.
func writeAITitleJSONL(t *testing.T, home, cwd, sessionID, title string) string {
	t.Helper()
	slug := cwdSlug(cwd)
	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	body := `{"type":"ai-title","aiTitle":"` + title + `"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	return path
}

func statOrFatal(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestCachedAITitle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := New(Options{})
	cwd := "/work/proj"
	id := "sess-1"
	path := writeAITitleJSONL(t, home, cwd, id, "First title")

	// Pin a known mtime so the cache key is stable.
	mtime := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if _, got := d.cachedTranscriptMeta(id, path, statOrFatal(t, path)); got != "First title" {
		t.Fatalf("first read = %q, want %q", got, "First title")
	}

	// Same-length rewrite with the SAME mtime: size and mtime both match, so the
	// cache must not re-read.
	if err := os.WriteFile(path, []byte(`{"type":"ai-title","aiTitle":"Fresh title"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if _, got := d.cachedTranscriptMeta(id, path, statOrFatal(t, path)); got != "First title" {
		t.Errorf("same size+mtime read = %q, want cached %q", got, "First title")
	}

	// Advance the mtime: the tail mismatches, so it rescans in full.
	newMtime := mtime.Add(time.Minute)
	if err := os.Chtimes(path, newMtime, newMtime); err != nil {
		t.Fatal(err)
	}
	last, got := d.cachedTranscriptMeta(id, path, statOrFatal(t, path))
	if got != "Fresh title" {
		t.Errorf("bumped-mtime read = %q, want %q", got, "Fresh title")
	}
	// No timestamped records, so last activity falls back to mtime.
	if !last.Equal(newMtime) {
		t.Errorf("last activity = %v, want mtime %v", last, newMtime)
	}
}

func TestCachedTranscriptMetaSameIDDifferentPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := New(Options{})
	id := "sess-shared"
	pathA := writeAITitleJSONL(t, home, "/work/a", id, "Title A")
	pathB := writeAITitleJSONL(t, home, "/work/b", id, "Title B")
	// Equal size and mtime, so only the path in the key keeps them apart.
	mtime := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	for _, p := range []string{pathA, pathB} {
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	if _, got := d.cachedTranscriptMeta(id, pathA, statOrFatal(t, pathA)); got != "Title A" {
		t.Errorf("path A = %q, want %q", got, "Title A")
	}
	if _, got := d.cachedTranscriptMeta(id, pathB, statOrFatal(t, pathB)); got != "Title B" {
		t.Errorf("path B = %q, want %q (state reused across paths)", got, "Title B")
	}
}
