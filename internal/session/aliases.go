package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// AliasStore persists user-defined session display names as a simple
// JSON map (session ID -> display name) at ~/.config/naviclaude/session-names.json.
type AliasStore struct {
	path    string
	mu      sync.RWMutex
	aliases map[string]string
}

// NewAliasStore creates a store that reads/writes to path.
// If path is empty, DefaultAliasPath() is used.
func NewAliasStore(path string) *AliasStore {
	if path == "" {
		path = DefaultAliasPath()
	}
	s := &AliasStore{path: path, aliases: make(map[string]string)}
	s.load()
	return s
}

// DefaultAliasPath returns ~/.config/naviclaude/session-names.json.
func DefaultAliasPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "naviclaude", "session-names.json")
}

// All returns a copy of all aliases.
func (s *AliasStore) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.aliases))
	for k, v := range s.aliases {
		out[k] = v
	}
	return out
}

// Set stores (or updates) a display name for a session and persists to disk.
func (s *AliasStore) Set(sessionID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		delete(s.aliases, sessionID)
	} else {
		s.aliases[sessionID] = name
	}
	return s.save()
}

func (s *AliasStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.aliases)
}

func (s *AliasStore) save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.aliases, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
