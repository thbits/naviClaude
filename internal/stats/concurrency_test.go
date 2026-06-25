package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestCacheConcurrentLoadSave exercises the Cache concurrency fix under
// `go test -race`. Many goroutines call Save, Load, and IsValid on a single
// shared Cache whose path is a temp file. Cache.mu serializes these so the
// on-disk file is never torn and the in-memory access is race-free; the atomic
// write-temp-then-rename in Save additionally guarantees a reader never observes
// a partial document. The primary signal is the absence of a reported race; the
// final assertion confirms the file is still a complete, parseable document.
func TestCacheConcurrentLoadSave(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{path: filepath.Join(dir, "stats-cache.json")}

	// Seed the cache so concurrent Loads have something valid to read.
	if err := c.Save(&Stats{TotalSessions: 1, TotalMessages: 10}, 1); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	var wg sync.WaitGroup

	// Writers.
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if err := c.Save(&Stats{TotalSessions: n, TotalMessages: n * 10}, n); err != nil {
					t.Errorf("Save: %v", err)
					return
				}
			}
		}(w)
	}

	// Readers via Load.
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// May be nil when fileCount n does not match the last writer;
				// that is fine. The point is the access is race-free.
				_ = c.Load(n)
			}
		}(r)
	}

	// Readers via IsValid (the other lock-taking read path).
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = c.IsValid(n)
			}
		}(r)
	}

	wg.Wait()

	// The file must still be a complete, parseable document after the storm.
	raw, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatalf("ReadFile after concurrency: %v", err)
	}
	var data cacheData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("file torn by concurrent Save: %v", err)
	}
	if data.Stats == nil {
		t.Error("final cache file has nil Stats")
	}

	// No leftover temp files: every Save's temp must have been renamed or removed.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "stats-cache.json" {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
}
