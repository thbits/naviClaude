package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Cache stores naviClaude's own computed stats to avoid re-scanning.
type Cache struct {
	// mu serializes Load/Save so overlapping computeStatsCmd goroutines cannot
	// tear the on-disk file (or read a half-written one) when they share a Cache.
	mu   sync.Mutex
	path string
}

type cacheData struct {
	Stats      *Stats    `json:"stats"`
	FileCount  int       `json:"file_count"`
	ComputedAt time.Time `json:"computed_at"`
}

// NewCache creates a cache at ~/.config/naviclaude/stats-cache.json.
func NewCache() *Cache {
	home, err := os.UserHomeDir()
	if err != nil {
		return &Cache{}
	}
	return &Cache{
		path: filepath.Join(home, ".config", "naviclaude", "stats-cache.json"),
	}
}

// IsValid returns true if the cache exists, is < 1 hour old, and the file count matches.
func (c *Cache) IsValid(fileCount int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isValidLocked(fileCount)
}

// isValidLocked is the lock-free core of IsValid. Callers must hold c.mu.
func (c *Cache) isValidLocked(fileCount int) bool {
	if c.path == "" {
		return false
	}
	data, err := c.load()
	if err != nil || data.Stats == nil {
		return false
	}
	if time.Since(data.ComputedAt) > time.Hour {
		return false
	}
	return data.FileCount == fileCount
}

// Load returns the cached stats, or nil if not valid.
func (c *Cache) Load(fileCount int) *Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Validate and read under a single lock so a concurrent Save cannot replace
	// the file between the validity check and the read.
	if !c.isValidLocked(fileCount) {
		return nil
	}
	data, err := c.load()
	if err != nil {
		return nil
	}
	return data.Stats
}

// Save writes computed stats to the cache file.
func (c *Cache) Save(stats *Stats, fileCount int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.path == "" {
		return nil
	}

	// Ensure directory exists.
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data := cacheData{
		Stats:      stats,
		FileCount:  fileCount,
		ComputedAt: time.Now(),
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Write atomically: write to a temp file in the same directory, then rename
	// over the destination. os.WriteFile truncates-then-writes in place, so two
	// overlapping Saves (or a Load racing a Save) could observe a torn/partial
	// file. A rename within the same filesystem is atomic and avoids that.
	tmp, err := os.CreateTemp(dir, "stats-cache-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails before the rename succeeds.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, c.path)
}

func (c *Cache) load() (*cacheData, error) {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return nil, err
	}
	var data cacheData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &data, nil
}
