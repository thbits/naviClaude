package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Cache stores naviClaude's own computed stats to avoid re-scanning.
type Cache struct {
	path string
}

type cacheData struct {
	Stats     *Stats    `json:"stats"`
	FileCount int       `json:"file_count"`
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
	if !c.IsValid(fileCount) {
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
	if c.path == "" {
		return nil
	}

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
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
	return os.WriteFile(c.path, raw, 0o644)
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
