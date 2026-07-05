# Live AI Session Title Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a Claude session's real, evolving title (the latest `ai-title` transcript record) in the naviClaude sidebar instead of the frozen derived placeholder.

**Architecture:** naviClaude already re-scans every session on a ~1s tick. The fix reads the correct source: the latest `ai-title` record from the session `.jsonl`. A small precedence helper decides between the per-PID `name` and the `aiTitle`; the active path (detector) reads it through an mtime-gated cache, and the closed path (history parser) captures it in its existing single pass.

**Tech Stack:** Go, standard library only (`bufio`, `encoding/json`, `os`). Tests are plain `go test`.

## Global Constraints

- Package under change: `internal/session`. Follow its existing patterns (mirror `lastMessageTime` / `cachedLastActivity`).
- All transcript scanners MUST use the shared `maxTranscriptLineBytes` buffer limit (`4 * 1024 * 1024`) so readers agree on which lines they can parse.
- No new dependencies, no config, no migration. Read-only against Claude's files.
- No emojis anywhere in code, comments, or commit messages.
- Run the full package test suite with `go test ./internal/session/...` after each task.

---

### Task 1: `lastAITitle` transcript reader

**Files:**
- Create: `internal/session/aititle.go`
- Test: `internal/session/aititle_test.go`

**Interfaces:**
- Consumes: `maxTranscriptLineBytes` (const in `internal/session/history.go`).
- Produces: `func lastAITitle(path string) string` — returns the `aiTitle` of the last `ai-title` record in the transcript, or `""`.

- [ ] **Step 1: Write the failing tests**

Create `internal/session/aititle_test.go`:

```go
package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastAITitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")

	// Two ai-title records; the physically-last one is the current title. The
	// file ends with an untimestamped meta record, as real transcripts do.
	lines := []string{
		`{"type":"user","timestamp":"2026-07-05T08:00:00Z"}`,
		`{"type":"ai-title","aiTitle":"First guess"}`,
		`{"type":"assistant","timestamp":"2026-07-05T09:00:00Z"}`,
		`{"type":"ai-title","aiTitle":"Configure Hindsight memory with local Postgres"}`,
		`{"type":"mode"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := lastAITitle(path)
	want := "Configure Hindsight memory with local Postgres"
	if got != want {
		t.Errorf("lastAITitle = %q, want %q (last ai-title wins)", got, want)
	}
}

func TestLastAITitleEmptyOrMissing(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		if got := lastAITitle(""); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if got := lastAITitle(filepath.Join(t.TempDir(), "nope.jsonl")); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("no ai-title records", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "meta.jsonl")
		os.WriteFile(path, []byte(`{"type":"user","timestamp":"2026-07-05T08:00:00Z"}`+"\n"), 0o644)
		if got := lastAITitle(path); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("malformed line skipped", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.jsonl")
		os.WriteFile(path, []byte("not json\n"+`{"type":"ai-title","aiTitle":"Good"}`+"\n"), 0o644)
		if got := lastAITitle(path); got != "Good" {
			t.Errorf("got %q, want %q", got, "Good")
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/ -run TestLastAITitle -v`
Expected: FAIL to compile with `undefined: lastAITitle`.

- [ ] **Step 3: Write the implementation**

Create `internal/session/aititle.go`:

```go
package session

import (
	"bufio"
	"encoding/json"
	"os"
)

// lastAITitle returns the aiTitle of the most recent "ai-title" record in a
// session's .jsonl transcript. Claude Code appends a fresh ai-title record each
// time it (re)generates the session's title, so the physically-last one is the
// current title. Returns "" when the file is missing, has no ai-title records,
// or cannot be read. Malformed and oversized lines are skipped, matching the
// tolerance of the other transcript scanners (lastMessageTime, parseSessionFile).
func lastAITitle(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var title string
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxTranscriptLineBytes)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			AITitle string `json:"aiTitle"`
		}
		if err := json.Unmarshal(line, &rec); err != nil || rec.Type != "ai-title" {
			continue
		}
		if rec.AITitle != "" {
			title = rec.AITitle
		}
	}
	return title
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/session/ -run TestLastAITitle -v`
Expected: PASS (both `TestLastAITitle` and `TestLastAITitleEmptyOrMissing`).

- [ ] **Step 5: Commit**

```bash
git add internal/session/aititle.go internal/session/aititle_test.go
git commit -m "feat(session): read latest ai-title from transcript"
```

---

### Task 2: Title precedence helper + `nameSource` metadata field

**Files:**
- Create: `internal/session/title.go`
- Test: `internal/session/title_test.go`
- Modify: `internal/session/detector.go` (the `sessionMetadata` struct, ~line 547-552)

**Interfaces:**
- Produces: `func resolveTitle(metaName, nameSource, aiTitle string) string`.
- Produces: `sessionMetadata.NameSource string` (json `nameSource`).
- Produces: `const derivedNameSource = "derived"`.

- [ ] **Step 1: Write the failing test**

Create `internal/session/title_test.go`:

```go
package session

import "testing"

func TestResolveTitle(t *testing.T) {
	tests := []struct {
		name       string
		metaName   string
		nameSource string
		aiTitle    string
		want       string
	}{
		{"derived name yields to aiTitle", "tomhalo-fe", "derived", "Configure Hindsight", "Configure Hindsight"},
		{"non-derived name wins over aiTitle", "my-name", "user", "Configure Hindsight", "my-name"},
		{"derived name with no aiTitle stays", "tomhalo-fe", "derived", "", "tomhalo-fe"},
		{"empty meta with aiTitle uses aiTitle", "", "", "Closed session title", "Closed session title"},
		{"all empty yields empty", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveTitle(tt.metaName, tt.nameSource, tt.aiTitle); got != tt.want {
				t.Errorf("resolveTitle(%q,%q,%q) = %q, want %q",
					tt.metaName, tt.nameSource, tt.aiTitle, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestResolveTitle -v`
Expected: FAIL to compile with `undefined: resolveTitle`.

- [ ] **Step 3: Write the helper**

Create `internal/session/title.go`:

```go
package session

// derivedNameSource is the nameSource value Claude Code writes in
// ~/.claude/sessions/<pid>.json for the auto-generated placeholder title
// (e.g. "<project>-<hash>"). Any other value (user-set, background session)
// is a real name that should win over the transcript's ai-title.
const derivedNameSource = "derived"

// resolveTitle picks a session's display title from the two on-disk sources:
// metaName/nameSource (from the per-PID <pid>.json) and aiTitle (the latest
// "ai-title" transcript record). Precedence, highest first:
//
//  1. metaName when nameSource is a real (non-derived) source
//  2. aiTitle
//  3. metaName (the derived placeholder, possibly "")
//
// User aliases sit above all of these and are applied separately by the app
// layer (applyAliases). Returning "" leaves the sidebar to fall back to
// Slug/ProjectName.
func resolveTitle(metaName, nameSource, aiTitle string) string {
	if metaName != "" && nameSource != derivedNameSource {
		return metaName
	}
	if aiTitle != "" {
		return aiTitle
	}
	return metaName
}
```

- [ ] **Step 4: Add the `NameSource` field to `sessionMetadata`**

In `internal/session/detector.go`, the struct currently reads:

```go
	SessionID       string `json:"sessionId"`
	Name            string `json:"name"`
	Status          string `json:"status"`          // busy|waiting|idle|shell
	StatusUpdatedAt int64  `json:"statusUpdatedAt"` // epoch ms
```

Change it to add `NameSource`:

```go
	SessionID       string `json:"sessionId"`
	Name            string `json:"name"`
	NameSource      string `json:"nameSource"`      // "derived" = auto placeholder
	Status          string `json:"status"`          // busy|waiting|idle|shell
	StatusUpdatedAt int64  `json:"statusUpdatedAt"` // epoch ms
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/session/ -run TestResolveTitle -v && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/session/title.go internal/session/title_test.go internal/session/detector.go
git commit -m "feat(session): add title precedence helper and nameSource field"
```

---

### Task 3: Wire the active-session path (`buildSession`) with an mtime-gated cache

**Files:**
- Modify: `internal/session/detector.go` (`Detector` struct ~154-165; `NewDetector` ~192-201; the by-ID fallback ~467-470; `buildSession` name assignment ~472-474 and the modTime block ~507-514; add `cachedAITitle` method + `aiTitleEntry` type near `cachedLastActivity` ~223-242)
- Test: `internal/session/aititle_cache_test.go`

**Interfaces:**
- Consumes: `resolveTitle` (Task 2), `lastAITitle` (Task 1), `sessionMetadata.NameSource` (Task 2), `SessionFilePath`, `sessionFileModTime` (existing).
- Produces: `func (d *Detector) cachedAITitle(sessionID, cwd string, mtime time.Time) string`.

- [ ] **Step 1: Write the failing cache test**

Create `internal/session/aititle_cache_test.go`:

```go
package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeAITitleJSONL writes a transcript with a single ai-title record under a
// fake ~/.claude/projects/<slug>/ tree so cachedAITitle's SessionFilePath read
// finds a real file. HOME must already be set to a temp dir by the caller.
func writeAITitleJSONL(t *testing.T, home, cwd, sessionID, title string) string {
	t.Helper()
	slug := cwdSlug(cwd)
	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	body := `{"type":"ai-title","aiTitle":"` + title + `"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	return path
}

func TestCachedAITitle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := NewDetector(nil, nil, 0, 0)
	cwd := "/work/proj"
	id := "sess-1"
	path := writeAITitleJSONL(t, home, cwd, id, "First title")

	// Pin a known mtime so the cache key is stable.
	mtime := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if got := d.cachedAITitle(id, cwd, mtime); got != "First title" {
		t.Fatalf("first read = %q, want %q", got, "First title")
	}

	// Rewrite the title but present the SAME mtime: the cache must return the
	// old value (a stale mtime means no re-scan).
	if err := os.WriteFile(path, []byte(`{"type":"ai-title","aiTitle":"Second title"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if got := d.cachedAITitle(id, cwd, mtime); got != "First title" {
		t.Errorf("same-mtime read = %q, want cached %q", got, "First title")
	}

	// Advance the mtime: the cache invalidates and re-reads the new title.
	newMtime := mtime.Add(time.Minute)
	if err := os.Chtimes(path, newMtime, newMtime); err != nil {
		t.Fatal(err)
	}
	if got := d.cachedAITitle(id, cwd, newMtime); got != "Second title" {
		t.Errorf("bumped-mtime read = %q, want %q", got, "Second title")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestCachedAITitle -v`
Expected: FAIL to compile with `undefined: (*Detector).cachedAITitle`.

- [ ] **Step 3: Add the cache field, entry type, and method**

In `internal/session/detector.go`, add a guarded cache field to the `Detector` struct alongside `lastActCache`:

```go
	lastActMu    sync.Mutex                   // guards lastActCache
	lastActCache map[string]lastActivityEntry // sessionID -> last-message time, keyed by file mtime

	aiTitleMu    sync.Mutex               // guards aiTitleCache
	aiTitleCache map[string]aiTitleEntry  // sessionID -> ai-title, keyed by file mtime
```

Add the entry type next to `lastActivityEntry`:

```go
// aiTitleEntry caches a session's ai-title alongside the transcript mtime it was
// read from, so the file is only re-scanned when it actually changes.
type aiTitleEntry struct {
	mtime time.Time
	title string
}
```

Initialize the map in `NewDetector`, next to `lastActCache: make(...)`:

```go
		lastActCache: make(map[string]lastActivityEntry),
		aiTitleCache: make(map[string]aiTitleEntry),
```

Add the method next to `cachedLastActivity`:

```go
// cachedAITitle returns the session's latest ai-title, scanning the transcript
// only when its mtime has changed since the last read (the common case on the
// detect tick is a cache hit). Mirrors cachedLastActivity so the per-second
// detect tick does not re-read the whole transcript on every pass.
func (d *Detector) cachedAITitle(sessionID, cwd string, mtime time.Time) string {
	d.aiTitleMu.Lock()
	defer d.aiTitleMu.Unlock()
	if e, ok := d.aiTitleCache[sessionID]; ok && e.mtime.Equal(mtime) {
		return e.title
	}
	title := lastAITitle(SessionFilePath(sessionID, cwd))
	d.aiTitleCache[sessionID] = aiTitleEntry{mtime: mtime, title: title}
	return title
}
```

- [ ] **Step 4: Run the cache test to verify it passes**

Run: `go test ./internal/session/ -run TestCachedAITitle -v`
Expected: PASS.

- [ ] **Step 5: Wire the title resolution into `buildSession`**

First, carry `NameSource` through the by-ID fallback. The block currently reads:

```go
		s.ID = extractSessionIDFallback(claudePID, pane.CurrentPath)
		if byID, ok := readSessionMetadataByID(s.ID); ok {
			meta.Name = byID.Name
		}
```

Change it to also copy the source:

```go
		s.ID = extractSessionIDFallback(claudePID, pane.CurrentPath)
		if byID, ok := readSessionMetadataByID(s.ID); ok {
			meta.Name = byID.Name
			meta.NameSource = byID.NameSource
		}
```

Next, DELETE the early name assignment (currently right after that block):

```go
	if meta.Name != "" {
		s.DisplayName = meta.Name
	}
```

Then replace the model/last-activity block:

```go
	if s.ID != "" {
		s.Model = d.cachedModel(s.ID, pane.CurrentPath)
		if modTime := sessionFileModTime(s.ID, pane.CurrentPath); !modTime.IsZero() {
			// Use the last message timestamp (not the file mtime) so a resumed
			// session shows when its conversation last happened, not "now".
			s.LastActivity = d.cachedLastActivity(s.ID, pane.CurrentPath, modTime)
		}
	}
```

with a version that also resolves the display title from the transcript's ai-title:

```go
	// Resolve the display title from the per-PID name and the transcript's
	// ai-title (see resolveTitle). aiTitle stays "" until a transcript exists on
	// disk, in which case resolveTitle falls back to the per-PID name.
	aiTitle := ""
	if s.ID != "" {
		s.Model = d.cachedModel(s.ID, pane.CurrentPath)
		if modTime := sessionFileModTime(s.ID, pane.CurrentPath); !modTime.IsZero() {
			// Use the last message timestamp (not the file mtime) so a resumed
			// session shows when its conversation last happened, not "now".
			s.LastActivity = d.cachedLastActivity(s.ID, pane.CurrentPath, modTime)
			aiTitle = d.cachedAITitle(s.ID, pane.CurrentPath, modTime)
		}
	}
	s.DisplayName = resolveTitle(meta.Name, meta.NameSource, aiTitle)
```

- [ ] **Step 6: Verify the whole package builds and passes**

Run: `go test ./internal/session/... && go build ./...`
Expected: PASS, clean build. (Existing detector/status tests must still pass — the derived `name` still surfaces when there is no ai-title.)

- [ ] **Step 7: Commit**

```bash
git add internal/session/detector.go internal/session/aititle_cache_test.go
git commit -m "feat(session): surface live ai-title for active sessions"
```

---

### Task 4: Wire the closed-session path (`parseSessionFile`)

**Files:**
- Modify: `internal/session/history.go` (`rawRecord` struct ~236-245; the parse loop ~280-344; the tail assignment ~357-365)
- Test: `internal/session/history_test.go` (add one test function)

**Interfaces:**
- Consumes: `rawRecord` (extended with `AITitle`).
- Produces: closed `Session.DisplayName` set to the latest `aiTitle` when present.

- [ ] **Step 1: Write the failing test**

Add to `internal/session/history_test.go`:

```go
func TestParseSessionFileSetsAITitle(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "11111111-2222-3333-4444-555555555555.jsonl")

	// An assistant record carries the cwd (required for parseSessionFile to keep
	// the session); two ai-title records bracket it and the last one must win.
	lines := []string{
		`{"type":"ai-title","aiTitle":"Old title"}`,
		`{"type":"assistant","timestamp":"2026-07-05T09:00:00Z","cwd":"/work/proj","message":{"model":"claude-opus-4-6","role":"assistant"}}`,
		`{"type":"ai-title","aiTitle":"Configure Hindsight memory with local Postgres"}`,
	}
	if err := os.WriteFile(filePath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sess, err := parseSessionFile(filePath)
	if err != nil {
		t.Fatalf("parseSessionFile error: %v", err)
	}
	if sess == nil {
		t.Fatal("parseSessionFile returned nil session")
	}
	want := "Configure Hindsight memory with local Postgres"
	if sess.DisplayName != want {
		t.Errorf("DisplayName = %q, want %q", sess.DisplayName, want)
	}
}
```

If `strings` is not already imported in `history_test.go`, add it to the import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestParseSessionFileSetsAITitle -v`
Expected: FAIL — `DisplayName = "" , want "Configure Hindsight memory with local Postgres"`.

- [ ] **Step 3: Extend `rawRecord`**

In `internal/session/history.go`, add the `AITitle` field to `rawRecord`:

```go
type rawRecord struct {
	SessionID string          `json:"sessionId"`
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Version   string          `json:"version"`
	Slug      string          `json:"slug"`
	AITitle   string          `json:"aiTitle"`
	Message   json.RawMessage `json:"message"`
}
```

- [ ] **Step 4: Capture the title in the parse loop and assign it**

In `parseSessionFile`, add `aiTitle` to the declared locals:

```go
	var (
		firstTime time.Time
		lastTime  time.Time
		modelSet  bool
		aiTitle   string
	)
```

Inside the scan loop (alongside the other `rec.Type` / field pickups, e.g. after the Slug pickup), capture the latest ai-title:

```go
		if rec.Type == "ai-title" && rec.AITitle != "" {
			aiTitle = rec.AITitle
		}
```

After the `sess.ID == "" || sess.CWD == ""` guard and before `return sess, nil` (next to the `LastActivity` assignment), set the display name:

```go
	// The latest ai-title record is the session's current title; surface it so a
	// closed session keeps the title Claude gave it instead of falling back to the
	// Slug/ProjectName placeholder in the sidebar.
	if aiTitle != "" {
		sess.DisplayName = aiTitle
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/session/... -v -run 'TestParseSessionFile|TestScan'`
Expected: PASS (the new test plus the existing parse/scan tests).

- [ ] **Step 6: Commit**

```bash
git add internal/session/history.go internal/session/history_test.go
git commit -m "feat(session): surface ai-title for closed sessions"
```

---

### Task 5: Full build, race check, and manual verification

**Files:** none (verification only)

- [ ] **Step 1: Full test suite with the race detector**

Run: `go test -race ./...`
Expected: PASS, no data races (the new `aiTitleCache` is guarded by `aiTitleMu`, mirroring `lastActCache`).

- [ ] **Step 2: Build the binary**

Run: `go build -o naviclaude ./cmd/naviclaude`
Expected: clean build.

- [ ] **Step 3: Manual check against a real session**

Run naviClaude in tmux. A live session whose `~/.claude/sessions/<pid>.json` has
`"nameSource":"derived"` (e.g. `tomhalo-fe`) must now show its real title from the
transcript (e.g. `Configure Hindsight memory with local Postgres`) within ~1s,
and the title must update if it changes mid-session.

- [ ] **Step 4: Commit any doc/verification notes if needed** (skip if nothing changed)

---

## Notes on scope

- **Alias precedence is unchanged and needs no new code.** `applyAliases`
  (`internal/app/app.go`) runs *after* the detector/history assign `DisplayName`
  and unconditionally overwrites it with the user's alias when one exists. Because
  the new logic only changes what `DisplayName` is set to *before* that step, a
  user alias still wins over an `aiTitle`, exactly as it previously won over
  `meta.Name`. The existing alias tests cover this behavior.

## Self-Review

- **Spec coverage:** Root-cause fix (read `aiTitle`) → Tasks 1, 3, 4. `lastAITitle`
  reader → Task 1. `nameSource` field → Task 2. Precedence → Task 2 (`resolveTitle`),
  applied in Tasks 3 & 4. Active call site → Task 3. Closed call site → Task 4.
  mtime cache → Task 3. Tests for reader/precedence/active/closed → Tasks 1-4.
  Alias-still-wins → Notes (no code change, existing coverage). Race safety → Task 5.
- **Placeholder scan:** none — every code step shows complete code.
- **Type consistency:** `resolveTitle(metaName, nameSource, aiTitle string) string`,
  `lastAITitle(path string) string`, `cachedAITitle(sessionID, cwd string, mtime time.Time) string`,
  `aiTitleEntry{mtime, title}`, `sessionMetadata.NameSource`, `rawRecord.AITitle`,
  `derivedNameSource` — used consistently across tasks.
