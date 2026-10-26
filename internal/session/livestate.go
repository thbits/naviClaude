package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LiveEntry is one running session and the tmux pane it lived in, as recorded
// for restoring after a tmux restart.
type LiveEntry struct {
	ID          string `json:"id"`
	Harness     string `json:"harness"`
	CWD         string `json:"cwd"`
	Project     string `json:"project"`
	SessionFile string `json:"session_file"`
	TmuxSession string `json:"tmux_session"`
	WindowIndex int    `json:"window_index"`
	PaneIndex   int    `json:"pane_index"`
}

// LiveSnapshot is the set of sessions running on one tmux server.
type LiveSnapshot struct {
	// ServerStart is the tmux server's #{start_time}; a different value means
	// the server restarted since the snapshot was taken.
	ServerStart int64       `json:"server_start"`
	SavedAt     time.Time   `json:"saved_at"`
	Sessions    []LiveEntry `json:"sessions"`
}

// LiveStateStore persists one LiveSnapshot per tmux server socket at
// <user cache dir>/naviclaude/live-sessions.json. Every call re-reads the file:
// naviClaude instances on other tmux servers share it.
type LiveStateStore struct {
	path    string
	mu      sync.Mutex
	loadErr error
}

// NewLiveStateStore creates a store at path (DefaultLiveStatePath() when empty)
// and records whether the existing file is readable.
func NewLiveStateStore(path string) *LiveStateStore {
	if path == "" {
		path = DefaultLiveStatePath()
	}
	s := &LiveStateStore{path: path}
	_, s.loadErr = s.read()
	return s
}

// DefaultLiveStatePath returns <user cache dir>/naviclaude/live-sessions.json.
func DefaultLiveStatePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "naviclaude", "live-sessions.json")
}

// Get returns the snapshot stored for the tmux server at socket.
func (s *LiveStateStore) Get(socket string) (LiveSnapshot, bool) {
	state, _ := s.read()
	snap, ok := state[socket]
	return snap, ok
}

// SaveIfChanged stores snap for socket, unless the server start and session set
// on disk are unchanged; other sockets' entries are kept as on disk.
func (s *LiveStateStore) SaveIfChanged(socket string, snap LiveSnapshot) (bool, error) {
	if s.path == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := lockFile(s.path + ".lock")
	if err != nil {
		return false, err
	}
	defer unlock()

	state, _ := s.read() // a corrupt file is replaced rather than blocking every save
	if state == nil {
		state = map[string]LiveSnapshot{}
	}
	if old, ok := state[socket]; ok && old.ServerStart == snap.ServerStart && slices.Equal(old.Sessions, snap.Sessions) {
		return false, nil
	}
	state[socket] = snap
	return true, s.write(state)
}

// LoadErr returns the error from reading the file at construction; nil when it
// was fine or missing.
func (s *LiveStateStore) LoadErr() error {
	return s.loadErr
}

// read loads every socket's snapshot; a missing file is an empty map.
func (s *LiveStateStore) read() (map[string]LiveSnapshot, error) {
	state := map[string]LiveSnapshot{}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

// write replaces the file atomically via a temp file and rename.
func (s *LiveStateStore) write(state map[string]LiveSnapshot) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".live-sessions-*.json.tmp")
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

// ParseTarget splits a "session:window.pane" target; the session name may itself
// contain ':' so the split is on the last one.
func ParseTarget(target string) (tmuxSession string, window, pane int, ok bool) {
	i := strings.LastIndex(target, ":")
	if i <= 0 {
		return "", 0, 0, false
	}
	winStr, paneStr, found := strings.Cut(target[i+1:], ".")
	if !found {
		return "", 0, 0, false
	}
	w, err1 := strconv.Atoi(winStr)
	p, err2 := strconv.Atoi(paneStr)
	if err1 != nil || err2 != nil {
		return "", 0, 0, false
	}
	return target[:i], w, p, true
}

// TryLockFile takes an exclusive, non-blocking flock on path; the OS drops it if
// the process dies, so a closed popup never leaves a stale lock.
func TryLockFile(path string) (release func(), err error) {
	return flockFile(path, syscall.LOCK_EX|syscall.LOCK_NB)
}

// lockFile waits for an exclusive flock on path.
func lockFile(path string) (release func(), err error) {
	return flockFile(path, syscall.LOCK_EX)
}

func flockFile(path string, how int) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
