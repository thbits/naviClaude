package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestCacheSaveAtomicAndConcurrent covers finding 6: Save is atomic (no torn
// file) and the Cache survives concurrent Save/Load without data races or a
// corrupt file. Run with -race for the strongest signal.
func TestCacheSaveAtomicAndConcurrent(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{path: filepath.Join(dir, "stats-cache.json")}

	st := &Stats{TotalSessions: 7, TotalMessages: 70}
	if err := c.Save(st, 3); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File is valid JSON and round-trips.
	raw, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var data cacheData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}
	if data.Stats == nil || data.Stats.TotalSessions != 7 {
		t.Errorf("round-trip mismatch: %+v", data.Stats)
	}

	// No leftover temp files in the directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "stats-cache.json" {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}

	// Hammer Save and Load concurrently; the file must always remain a complete,
	// parseable document (the rename guarantees readers never see a partial one).
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.Save(&Stats{TotalSessions: n, TotalMessages: n * 10}, n)
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.Load(n)
			}
		}(i)
	}
	wg.Wait()

	// Final file must still be a complete, parseable document.
	raw, err = os.ReadFile(c.path)
	if err != nil {
		t.Fatalf("ReadFile after concurrency: %v", err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("file torn by concurrent Save: %v", err)
	}
}

// TestCacheEmptyPathNoop covers the path=="" guard: Save/Load on a Cache with
// no path are safe no-ops.
func TestCacheEmptyPathNoop(t *testing.T) {
	c := &Cache{}
	if err := c.Save(&Stats{}, 1); err != nil {
		t.Errorf("Save on empty-path cache: %v", err)
	}
	if got := c.Load(1); got != nil {
		t.Errorf("Load on empty-path cache = %v, want nil", got)
	}
	if c.IsValid(1) {
		t.Error("IsValid on empty-path cache = true, want false")
	}
}
