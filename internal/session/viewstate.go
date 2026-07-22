package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// ViewState is the persisted UI state restored when naviClaude reopens.
type ViewState struct {
	// LastSessionID is the ID of the session focused when the app last closed.
	LastSessionID string `json:"last_session_id"`
	// CollapsedGroups maps a manually-toggled group name to its collapsed value.
	// Only groups the user explicitly toggled are stored.
	CollapsedGroups map[string]bool `json:"collapsed_groups"`
}

// ViewStateStore persists ViewState as JSON at
// ~/.config/naviclaude/view-state.json, mirroring AliasStore.
type ViewStateStore struct {
	path    string
	mu      sync.RWMutex
	state   ViewState
	loadErr error
}

// NewViewStateStore creates a store that reads/writes to path. If path is empty,
// DefaultViewStatePath() is used. Loads any existing state on construction.
func NewViewStateStore(path string) *ViewStateStore {
	if path == "" {
		path = DefaultViewStatePath()
	}
	s := &ViewStateStore{path: path}
	s.load()
	return s
}

// DefaultViewStatePath returns ~/.config/naviclaude/view-state.json.
func DefaultViewStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "naviclaude", "view-state.json")
}

// Get returns a deep copy of the current view state.
func (s *ViewStateStore) Get() ViewState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := ViewState{LastSessionID: s.state.LastSessionID}
	if s.state.CollapsedGroups != nil {
		out.CollapsedGroups = make(map[string]bool, len(s.state.CollapsedGroups))
		for k, v := range s.state.CollapsedGroups {
			out.CollapsedGroups[k] = v
		}
	}
	return out
}

// Save replaces the stored state and persists it to disk.
func (s *ViewStateStore) Save(vs ViewState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = vs
	return s.save()
}

// LoadErr returns the error (if any) from the most recent load attempt. It is
// nil when the last load succeeded or the file did not exist.
func (s *ViewStateStore) LoadErr() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

func (s *ViewStateStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		// A missing file is the normal first-run case, not corruption.
		s.loadErr = nil
		return
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		s.loadErr = err
		return
	}
	s.loadErr = nil
}

// save writes the state atomically: serialize to a temp file in the target's
// directory, then os.Rename over the target. Callers hold s.mu.
func (s *ViewStateStore) save() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".view-state-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	tmpName = ""
	return nil
}
