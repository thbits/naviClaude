package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testSocket = "/tmp/tmux-501/default"

func TestLiveStateRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live-sessions.json")
	want := LiveSnapshot{
		ServerStart: 100,
		Sessions:    []LiveEntry{{ID: "a", TmuxSession: "s", WindowIndex: 2, PaneIndex: 1}},
	}
	if _, err := NewLiveStateStore(path).SaveIfChanged(testSocket, want); err != nil {
		t.Fatalf("SaveIfChanged: %v", err)
	}

	got, ok := NewLiveStateStore(path).Get(testSocket)
	if !ok {
		t.Fatal("Get after reload: ok = false")
	}
	if got.ServerStart != 100 || len(got.Sessions) != 1 || got.Sessions[0] != want.Sessions[0] {
		t.Fatalf("Get = %+v, want %+v", got, want)
	}
}

func TestLiveStateKeyedBySocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live-sessions.json")
	s := NewLiveStateStore(path)
	if _, err := s.SaveIfChanged("A", LiveSnapshot{ServerStart: 1}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("B"); ok {
		t.Fatal("Get(B) found server A's snapshot")
	}
	if _, err := s.SaveIfChanged("B", LiveSnapshot{ServerStart: 2}); err != nil {
		t.Fatal(err)
	}

	reloaded := NewLiveStateStore(path)
	a, okA := reloaded.Get("A")
	b, okB := reloaded.Get("B")
	if !okA || a.ServerStart != 1 || !okB || b.ServerStart != 2 {
		t.Fatalf("after reload A=%+v(%v) B=%+v(%v)", a, okA, b, okB)
	}
}

func TestLiveStateMissingFile(t *testing.T) {
	s := NewLiveStateStore(filepath.Join(t.TempDir(), "missing.json"))
	if _, ok := s.Get(testSocket); ok {
		t.Fatal("Get on missing file: ok = true")
	}
	if s.LoadErr() != nil {
		t.Fatalf("LoadErr = %v, want nil", s.LoadErr())
	}
}

func TestLiveStateCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live-sessions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if NewLiveStateStore(path).LoadErr() == nil {
		t.Fatal("LoadErr = nil for corrupt file")
	}
}

func TestLiveStateSaveIfChangedSkipsSameSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live-sessions.json")
	s := NewLiveStateStore(path)
	snap := LiveSnapshot{ServerStart: 7, SavedAt: time.Unix(1, 0), Sessions: []LiveEntry{{ID: "x"}}}
	wrote, err := s.SaveIfChanged(testSocket, snap)
	if err != nil || !wrote {
		t.Fatalf("first save: wrote=%v err=%v", wrote, err)
	}
	before, _ := os.ReadFile(path)

	snap.SavedAt = time.Unix(2, 0)
	wrote, err = s.SaveIfChanged(testSocket, snap)
	if err != nil || wrote {
		t.Fatalf("unchanged save: wrote=%v err=%v, want false", wrote, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("file rewritten for an unchanged session set")
	}
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		in        string
		sess      string
		win, pane int
		ok        bool
	}{
		{"my sess:3.1", "my sess", 3, 1, true},
		{"a:b:2.0", "a:b", 2, 0, true},
		{"", "", 0, 0, false},
		{"nocolon", "", 0, 0, false},
		{"s:x.y", "", 0, 0, false},
	}
	for _, tt := range tests {
		sess, win, pane, ok := ParseTarget(tt.in)
		if ok != tt.ok || (ok && (sess != tt.sess || win != tt.win || pane != tt.pane)) {
			t.Errorf("ParseTarget(%q) = (%q, %d, %d, %v), want (%q, %d, %d, %v)",
				tt.in, sess, win, pane, ok, tt.sess, tt.win, tt.pane, tt.ok)
		}
	}
}

// A second process with an older copy of the file must not put back stale
// entries for other sockets.
func TestLiveStateTwoStoresKeepEachOthersSockets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live-sessions.json")
	if _, err := NewLiveStateStore(path).SaveIfChanged("default", LiveSnapshot{ServerStart: 1}); err != nil {
		t.Fatal(err)
	}
	a := NewLiveStateStore(path)
	b := NewLiveStateStore(path)

	if _, err := a.SaveIfChanged("default", LiveSnapshot{ServerStart: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.SaveIfChanged("other", LiveSnapshot{ServerStart: 9}); err != nil {
		t.Fatal(err)
	}

	got, _ := NewLiveStateStore(path).Get("default")
	if got.ServerStart != 2 {
		t.Fatalf("default ServerStart = %d after the other store saved, want 2", got.ServerStart)
	}
}

// Get reads what another process wrote after this store was created.
func TestLiveStateGetSeesOtherProcessWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live-sessions.json")
	reader := NewLiveStateStore(path)
	if _, err := NewLiveStateStore(path).SaveIfChanged("s", LiveSnapshot{ServerStart: 5}); err != nil {
		t.Fatal(err)
	}
	if got, ok := reader.Get("s"); !ok || got.ServerStart != 5 {
		t.Fatalf("Get = %+v (ok=%v), want the other store's write", got, ok)
	}
}

func TestTryLockFileExcludesSecondHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restore.lock")
	release, err := TryLockFile(path)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if _, err := TryLockFile(path); err == nil {
		t.Fatal("second lock succeeded while the first is held")
	}
	release()
	release2, err := TryLockFile(path)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	release2()
}
