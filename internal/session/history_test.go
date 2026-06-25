package session

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// writeClosedSessionFile writes a minimal valid session .jsonl with one
// assistant record (so parseSessionFile yields a Session with a CWD) and sets
// the file's mtime. Returns the session ID (the filename stem).
func writeClosedSessionFile(t *testing.T, projectsDir, slug, sessionID, cwd string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(projectsDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	filePath := filepath.Join(dir, sessionID+".jsonl")
	records := []map[string]interface{}{
		{
			"type":      "assistant",
			"timestamp": mtime.UTC().Format(time.RFC3339Nano),
			"cwd":       cwd,
			"message": map[string]interface{}{
				"role":  "assistant",
				"model": "claude-opus-4-6",
			},
		},
	}
	writeJSONL(t, filePath, records)
	if err := os.Chtimes(filePath, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func TestScanClosedAndAll(t *testing.T) {
	claudeDir := t.TempDir()
	projectsDir := filepath.Join(claudeDir, "projects")

	now := time.Now()
	// recent: within the closed window. old: outside it.
	writeClosedSessionFile(t, projectsDir, "-home-user-recent", "recent-session", "/home/user/recent", now.Add(-1*time.Hour))
	writeClosedSessionFile(t, projectsDir, "-home-user-old", "old-session", "/home/user/old", now.Add(-72*time.Hour))

	s := &HistoryScanner{claudeDir: claudeDir}

	closed, all, err := s.ScanClosedAndAll(24, nil)
	if err != nil {
		t.Fatalf("ScanClosedAndAll: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("all = %d sessions, want 2", len(all))
	}
	if len(closed) != 1 {
		t.Fatalf("closed = %d sessions, want 1", len(closed))
	}
	if closed[0].ID != "recent-session" {
		t.Errorf("closed[0].ID = %q, want %q", closed[0].ID, "recent-session")
	}

	// closed must be a subset of all, sharing the same pointers.
	found := false
	for _, a := range all {
		if a == closed[0] {
			found = true
			break
		}
	}
	if !found {
		t.Error("closed[0] pointer not present in all (should share pointers)")
	}
}

func TestScanClosedAndAllNoTimeFilter(t *testing.T) {
	claudeDir := t.TempDir()
	projectsDir := filepath.Join(claudeDir, "projects")

	now := time.Now()
	writeClosedSessionFile(t, projectsDir, "-a-recent", "recent", "/a/recent", now.Add(-1*time.Hour))
	writeClosedSessionFile(t, projectsDir, "-a-ancient", "ancient", "/a/ancient", now.Add(-1000*time.Hour))

	s := &HistoryScanner{claudeDir: claudeDir}

	// closedHours <= 0 means no time filter: closed == all.
	closed, all, err := s.ScanClosedAndAll(0, nil)
	if err != nil {
		t.Fatalf("ScanClosedAndAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %d, want 2", len(all))
	}
	if len(closed) != 2 {
		t.Fatalf("closed = %d, want 2 (no time filter)", len(closed))
	}
}

// TestScanClosedAndAllMatchesSeparateScans verifies the single-pass method
// returns the same sessions as the existing ScanClosed/ScanAll methods.
func TestScanClosedAndAllMatchesSeparateScans(t *testing.T) {
	claudeDir := t.TempDir()
	projectsDir := filepath.Join(claudeDir, "projects")

	now := time.Now()
	writeClosedSessionFile(t, projectsDir, "-p-one", "one", "/p/one", now.Add(-30*time.Minute))
	writeClosedSessionFile(t, projectsDir, "-p-two", "two", "/p/two", now.Add(-10*time.Hour))
	writeClosedSessionFile(t, projectsDir, "-p-three", "three", "/p/three", now.Add(-100*time.Hour))

	s := &HistoryScanner{claudeDir: claudeDir}

	closedHours := 24.0
	wantClosed, err := s.ScanClosed(closedHours, nil)
	if err != nil {
		t.Fatalf("ScanClosed: %v", err)
	}
	wantAll, err := s.ScanAll(nil)
	if err != nil {
		t.Fatalf("ScanAll: %v", err)
	}

	gotClosed, gotAll, err := s.ScanClosedAndAll(closedHours, nil)
	if err != nil {
		t.Fatalf("ScanClosedAndAll: %v", err)
	}

	if len(gotClosed) != len(wantClosed) {
		t.Errorf("closed count = %d, want %d", len(gotClosed), len(wantClosed))
	}
	if len(gotAll) != len(wantAll) {
		t.Errorf("all count = %d, want %d", len(gotAll), len(wantAll))
	}

	idSet := func(ss []*Session) map[string]bool {
		m := make(map[string]bool, len(ss))
		for _, x := range ss {
			m[x.ID] = true
		}
		return m
	}
	wc, gc := idSet(wantClosed), idSet(gotClosed)
	for id := range wc {
		if !gc[id] {
			t.Errorf("closed missing session %q", id)
		}
	}
}

func TestScanClosedAndAllExcludesActive(t *testing.T) {
	claudeDir := t.TempDir()
	projectsDir := filepath.Join(claudeDir, "projects")

	now := time.Now()
	writeClosedSessionFile(t, projectsDir, "-x-active", "active-one", "/x/active", now.Add(-5*time.Minute))
	writeClosedSessionFile(t, projectsDir, "-x-closed", "closed-one", "/x/closed", now.Add(-5*time.Minute))

	s := &HistoryScanner{claudeDir: claudeDir}
	active := map[string]bool{"active-one": true}

	closed, all, err := s.ScanClosedAndAll(24, active)
	if err != nil {
		t.Fatalf("ScanClosedAndAll: %v", err)
	}
	for _, ss := range all {
		if ss.ID == "active-one" {
			t.Error("active session should be excluded from all")
		}
	}
	if len(closed) != 1 || closed[0].ID != "closed-one" {
		t.Errorf("closed = %v, want only closed-one", closed)
	}
}

// TestLoadHistoryIndexConcurrent exercises the cache lock under -race. It does
// not assert specific values; the point is that concurrent calls do not trip
// the race detector on cachedIndex/cachedIndexAge.
func TestLoadHistoryIndexConcurrent(t *testing.T) {
	claudeDir := t.TempDir()
	// Write a small history.jsonl so the index is non-empty.
	historyPath := filepath.Join(claudeDir, "history.jsonl")
	content := `{"sessionId":"s1","display":"first prompt","timestamp":1}` + "\n" +
		`{"sessionId":"s2","display":"second prompt","timestamp":2}` + "\n"
	if err := os.WriteFile(historyPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write history.jsonl: %v", err)
	}

	s := &HistoryScanner{claudeDir: claudeDir}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx, err := s.LoadHistoryIndex()
			if err != nil {
				t.Errorf("LoadHistoryIndex: %v", err)
				return
			}
			// Read the published map (must not be mutated by the scanner).
			_ = idx["s1"]
		}()
	}
	wg.Wait()

	idx, err := s.LoadHistoryIndex()
	if err != nil {
		t.Fatalf("LoadHistoryIndex final: %v", err)
	}
	if idx["s1"] != "first prompt" {
		t.Errorf("idx[s1] = %q, want %q", idx["s1"], "first prompt")
	}
}
