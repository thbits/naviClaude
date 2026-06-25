package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// These tests exercise the concurrency fixes in the session package and are
// meant to be run under `go test -race`. They spin up many goroutines that hit
// the now-mutex-guarded maps (Detector.modelCache, StatusTracker.lastWorking,
// HistoryScanner.cachedIndex) so the race detector can catch any unguarded
// access. They assert correctness where a deterministic answer exists, but the
// primary signal is the absence of a reported data race.

// writeFakeSessionJSONL writes a minimal session .jsonl under a fake
// ~/.claude/projects/<slug>/ tree so the model-extraction path in
// Detector.cachedModel reads a real file. It returns the sessionID.
func writeFakeSessionJSONL(t *testing.T, home, cwd, sessionID, modelID string) {
	t.Helper()
	slug := cwdSlug(cwd)
	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	rec := map[string]interface{}{
		"type":      "assistant",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"message":   map[string]string{"model": modelID, "role": "assistant"},
	}
	line, _ := json.Marshal(rec)
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
}

// TestDetectorCachedModelConcurrent hammers Detector.cachedModel from many
// goroutines across several sessionIDs. cachedModel guards modelCache with
// modelCacheMu; this verifies the guarded check-extract-store sequence is
// race-free and that the cached value is correct. cachedModel is the only place
// Detect mutates the shared modelCache map, so it is the path the race detector
// must clear.
func TestDetectorCachedModelConcurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := NewDetector(nil, nil, 0, 0)

	const nSessions = 8
	cwds := make([]string, nSessions)
	ids := make([]string, nSessions)
	for i := 0; i < nSessions; i++ {
		cwds[i] = filepath.Join("/work", fmt.Sprintf("proj%d", i))
		ids[i] = fmt.Sprintf("session-%d", i)
		writeFakeSessionJSONL(t, home, cwds[i], ids[i], "claude-opus-4-6")
	}

	var wg sync.WaitGroup
	// Many goroutines, each repeatedly resolving the model for every session,
	// so reads of an already-cached entry race with the first writer.
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < 20; r++ {
				for i := 0; i < nSessions; i++ {
					if got := d.cachedModel(ids[i], cwds[i]); got != "" && got != "opus" {
						t.Errorf("cachedModel(%q) = %q, want opus or empty", ids[i], got)
						return
					}
				}
			}
		}()
	}
	wg.Wait()

	// After the storm every session resolves to opus from the cache.
	for i := 0; i < nSessions; i++ {
		if got := d.cachedModel(ids[i], cwds[i]); got != "opus" {
			t.Errorf("final cachedModel(%q) = %q, want opus", ids[i], got)
		}
	}
}

// TestStatusTrackerResolveConcurrent hammers StatusTracker.Resolve and Prune
// from many goroutines across overlapping targets. Resolve writes lastWorking
// under st.mu and Prune deletes from it under the same lock; this verifies the
// guarded map access is race-free under concurrent reads, writes, and deletes.
func TestStatusTrackerResolveConcurrent(t *testing.T) {
	st := NewStatusTracker(10 * time.Second)

	targets := []string{"a:0.0", "b:0.0", "c:0.0", "d:0.0", "e:0.0"}
	base := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	// Resolvers: each goroutine cycles through every target with a mix of
	// signals, writing into and reading from lastWorking concurrently.
	for g := 0; g < 24; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			signals := []PaneSignal{SignalWorking, SignalNone, SignalWaiting}
			for r := 0; r < 200; r++ {
				for i, tgt := range targets {
					sig := signals[(g+r+i)%len(signals)]
					now := base.Add(time.Duration(r) * time.Second)
					_ = st.Resolve(tgt, sig, (g+i)%2 == 0, (r+i)%2 == 0, now)
				}
			}
		}(g)
	}
	// Pruners: concurrently drop a rotating subset of targets so deletes race
	// with the resolvers' inserts/reads on the same map.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for r := 0; r < 200; r++ {
				active := map[string]bool{targets[(g+r)%len(targets)]: true}
				st.Prune(active)
			}
		}(g)
	}
	wg.Wait()

	// Sanity: a deterministic call still returns the expected status after the
	// concurrent storm (state machine remains intact).
	if got := st.Resolve("a:0.0", SignalWaiting, false, false, base); got != StatusWaiting {
		t.Errorf("post-storm Resolve waiting = %v, want StatusWaiting", got)
	}
}

// TestResolveStatusConcurrent drives Detector.resolveStatus from many goroutines.
// resolveStatus is the per-session status path inside Detect: it reads the
// transcript mtime (filesystem, via the fake HOME), computes subtree CPU from a
// shared ProcessTree, and calls the mutex-guarded StatusTracker.Resolve. This
// exercises the same shared state Detect touches per pane, without depending on
// a real tmux server (subprocess sessions skip pane capture). Native status is
// left empty so the signal-based fallback path (which uses the tracker) runs.
func TestResolveStatusConcurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := NewDetector(nil, nil, 0, 0)

	// A shared process tree read concurrently by SubtreeCPU (read-only).
	tree := &ProcessTree{
		children: map[int][]int{1000: {1001}},
		names:    map[int]string{1000: "claude", 1001: "node"},
		ppid:     map[int]int{1000: 1, 1001: 1000},
		cpu:      map[int]float64{1000: 1.0, 1001: 30.0},
		rss:      map[int]float64{1000: 0, 1001: 0},
	}

	const nSessions = 6
	sessions := make([]*Session, nSessions)
	for i := 0; i < nSessions; i++ {
		cwd := filepath.Join("/work", fmt.Sprintf("rsproj%d", i))
		id := fmt.Sprintf("rs-session-%d", i)
		writeFakeSessionJSONL(t, home, cwd, id, "claude-sonnet-4-6")
		sessions[i] = &Session{
			ID:         id,
			CWD:        cwd,
			PID:        1000,
			TmuxTarget: fmt.Sprintf("rs%d:0.0", i),
			// Subprocess avoids the tmux capture-pane shell-out so the test does
			// not depend on a running tmux server; the tracker path still runs.
			Subprocess:       true,
			SubprocessParent: "nvim",
		}
	}

	var wg sync.WaitGroup
	for g := 0; g < 24; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < 50; r++ {
				for _, s := range sessions {
					status := d.resolveStatus(s, tree)
					// resolveStatus must return a valid, defined status.
					switch status {
					case StatusActive, StatusIdle, StatusWaiting, StatusClosed:
					default:
						t.Errorf("resolveStatus returned undefined status %v", status)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

// TestLoadHistoryIndexConcurrentFakeHome exercises HistoryScanner.LoadHistoryIndex
// from many goroutines using a HistoryScanner rooted at a temp dir (the package
// already has TestLoadHistoryIndexConcurrent; this variant additionally builds
// the scanner the production way via NewHistoryScanner with a fake HOME and
// asserts the cached map content is correct after the storm). LoadHistoryIndex
// guards cachedIndex/cachedIndexAge with cacheMu.
func TestLoadHistoryIndexConcurrentFakeHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	content := `{"sessionId":"h1","display":"alpha prompt","timestamp":1}` + "\n" +
		`{"sessionId":"h2","display":"beta prompt","timestamp":2}` + "\n" +
		`{"sessionId":"h1","display":"alpha later","timestamp":3}` + "\n"
	if err := os.WriteFile(filepath.Join(claudeDir, "history.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write history.jsonl: %v", err)
	}

	// NewHistoryScanner("") resolves the base via claudeHomeDir() -> fake HOME.
	s, err := NewHistoryScanner("")
	if err != nil {
		t.Fatalf("NewHistoryScanner: %v", err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < 30; r++ {
				idx, err := s.LoadHistoryIndex()
				if err != nil {
					t.Errorf("LoadHistoryIndex: %v", err)
					return
				}
				// Read the published map without mutating it.
				_ = idx["h1"]
				_ = idx["h2"]
			}
		}()
	}
	wg.Wait()

	idx, err := s.LoadHistoryIndex()
	if err != nil {
		t.Fatalf("final LoadHistoryIndex: %v", err)
	}
	// First (earliest) entry per session is kept.
	if idx["h1"] != "alpha prompt" {
		t.Errorf("idx[h1] = %q, want %q", idx["h1"], "alpha prompt")
	}
	if idx["h2"] != "beta prompt" {
		t.Errorf("idx[h2] = %q, want %q", idx["h2"], "beta prompt")
	}
}
