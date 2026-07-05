package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeAITitleJSONL writes a transcript with a single ai-title record under a
// fake ~/.claude/projects/<slug>/ tree so cachedAITitle's SessionFilePath read
// finds a real file. HOME must already be set to a temp dir by the caller.
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

func TestCachedAITitle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := NewDetector(nil, nil, 0, 0)
	cwd := "/work/proj"
	id := "sess-1"
	path := writeAITitleJSONL(t, home, cwd, id, "First title")

	// Pin a known mtime so the cache key is stable.
	mtime := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if got := d.cachedAITitle(id, cwd, mtime); got != "First title" {
		t.Fatalf("first read = %q, want %q", got, "First title")
	}

	// Rewrite the title but present the SAME mtime: the cache must return the
	// old value (a stale mtime means no re-scan).
	if err := os.WriteFile(path, []byte(`{"type":"ai-title","aiTitle":"Second title"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if got := d.cachedAITitle(id, cwd, mtime); got != "First title" {
		t.Errorf("same-mtime read = %q, want cached %q", got, "First title")
	}

	// Advance the mtime: the cache invalidates and re-reads the new title.
	newMtime := mtime.Add(time.Minute)
	if err := os.Chtimes(path, newMtime, newMtime); err != nil {
		t.Fatal(err)
	}
	if got := d.cachedAITitle(id, cwd, newMtime); got != "Second title" {
		t.Errorf("bumped-mtime read = %q, want %q", got, "Second title")
	}
}
