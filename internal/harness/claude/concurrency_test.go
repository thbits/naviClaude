package claude

import (
	"encoding/json"
	"fmt"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// These tests exercise the concurrency fixes in the session package and are
// meant to be run under `go test -race`. They spin up many goroutines that hit
// the now-mutex-guarded maps (Harness.modelCache, StatusTracker.lastWorking,
// HistoryScanner.cachedIndex) so the race detector can catch any unguarded
// access. They assert correctness where a deterministic answer exists, but the
// primary signal is the absence of a reported data race.

// writeFakeSessionJSONL writes a minimal session .jsonl under a fake
// ~/.claude/projects/<slug>/ tree so the model-extraction path in
// Harness.cachedModel reads a real file. It returns the sessionID.
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

// TestDetectorCachedModelConcurrent hammers Harness.cachedModel from many
// goroutines across several sessionIDs. cachedModel guards modelCache with
// modelCacheMu; this verifies the guarded check-extract-store sequence is
// race-free and that the cached value is correct. cachedModel is the only place
// Detect mutates the shared modelCache map, so it is the path the race detector
// must clear.
func TestDetectorCachedModelConcurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := New(Options{})

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

// TestResolveStatusConcurrent drives Detector.resolveStatus from many goroutines.
// ResolveStatus is the per-session status path inside Detect: it reads the
// transcript mtime (filesystem, via the fake HOME), computes subtree CPU from a
// shared ProcessTree, and calls the mutex-guarded StatusTracker.Resolve. This
// exercises the same shared state Detect touches per pane, without depending on
// a real tmux server (subprocess sessions skip pane capture). Native status is
// left empty so the signal-based fallback path (which uses the tracker) runs.
func TestResolveStatusConcurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	h := New(Options{})
	d := harness.NewDetector(nil, harness.NewRegistry(h), 0, 0)

	// A shared process tree read concurrently by SubtreeCPU (read-only).
	tree := session.NewProcessTree([]session.Proc{
		{PID: 1000, PPID: 1, Name: "claude", CPU: 1.0},
		{PID: 1001, PPID: 1000, Name: "node", CPU: 30.0},
	})

	const nSessions = 6
	sessions := make([]*session.Session, nSessions)
	for i := 0; i < nSessions; i++ {
		cwd := filepath.Join("/work", fmt.Sprintf("rsproj%d", i))
		id := fmt.Sprintf("rs-session-%d", i)
		writeFakeSessionJSONL(t, home, cwd, id, "claude-sonnet-4-6")
		sessions[i] = &session.Session{
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
					status := d.ResolveStatus(h, s, tree)
					// ResolveStatus must return a valid, defined status.
					switch status {
					case session.StatusActive, session.StatusIdle, session.StatusWaiting, session.StatusClosed:
					default:
						t.Errorf("ResolveStatus returned undefined status %v", status)
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
