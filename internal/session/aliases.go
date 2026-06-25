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

	// loadErr records the most recent error encountered while unmarshaling the
	// on-disk file. It is retained (rather than silently discarded) so callers
	// can distinguish a corrupt file from an empty one via LoadErr. A nil value
	// means the last load succeeded or the file did not exist.
	loadErr error
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

// LoadErr returns the error (if any) from the most recent attempt to unmarshal
// the on-disk aliases file. It is nil when the last load succeeded or the file
// did not exist. Lets callers detect a corrupt file rather than treating it as
// an empty alias set.
func (s *AliasStore) LoadErr() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

func (s *AliasStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		// A missing file is the normal first-run case, not corruption.
		s.loadErr = nil
		return
	}
	if err := json.Unmarshal(data, &s.aliases); err != nil {
		// Preserve the existing (empty) map but remember the corruption so it
		// is not silently hidden.
		s.loadErr = err
		return
	}
	s.loadErr = nil
}

// save writes the aliases atomically: it serializes to a temp file in the same
// directory as the target, then os.Rename over the target. The rename is atomic
// on the same filesystem, so a reader never observes a partially written file
// and a crash mid-write cannot corrupt the existing aliases. Callers hold s.mu.
func (s *AliasStore) save() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.aliases, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".session-names-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we fail before the rename succeeds.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	// Rename succeeded; suppress the deferred cleanup.
	tmpName = ""
	return nil
}
