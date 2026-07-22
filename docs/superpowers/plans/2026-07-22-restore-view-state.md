# Restore View State on Reopen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Optionally restore the last focused session and group open/closed state when naviClaude reopens.

**Architecture:** Two opt-in config flags gate restoration. A small JSON state store (`view-state.json`, modeled on the existing `AliasStore`) persists the last focused session ID and the set of manually-toggled group states. The sidebar exposes export/seed methods for group state; the app seeds group state in `New()`, restores the session after sessions load (with a one-shot guard), and writes state at quit.

**Tech Stack:** Go, Bubble Tea, `encoding/json`, `gopkg.in/yaml.v3`.

## Global Constraints

- No emojis anywhere in code, comments, or output.
- Both config flags default to `false` (opt-in); existing behavior is unchanged unless enabled.
- State store lives at `~/.config/naviclaude/view-state.json`, following the `AliasStore` pattern (atomic temp-file + rename save; missing file is not an error; corrupt file recorded in `loadErr`).
- Write-vs-apply policy: on quit always write the full current view state; on startup apply each field only if its toggle is on.
- Session `ID` is the stable `.jsonl` UUID and is the key used to re-select a session.
- Group state is keyed by group name (tmux session name); only user-toggled groups are persisted.

---

### Task 1: Config flags

**Files:**
- Modify: `internal/config/config.go:20-39` (struct), `:58-90` (DefaultConfig)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.FocusLastSession bool` (yaml `focus_last_session`), `Config.RememberGroupState bool` (yaml `remember_group_state`). Both default `false`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/config_test.go`:

```go
func TestDefaultConfigViewStateFlags(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.FocusLastSession != false {
		t.Errorf("FocusLastSession default = %v, want false", cfg.FocusLastSession)
	}
	if cfg.RememberGroupState != false {
		t.Errorf("RememberGroupState default = %v, want false", cfg.RememberGroupState)
	}
}

func TestLoadViewStateFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "focus_last_session: true\nremember_group_state: true\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !cfg.FocusLastSession {
		t.Error("FocusLastSession = false, want true")
	}
	if !cfg.RememberGroupState {
		t.Error("RememberGroupState = false, want true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run ViewState -v`
Expected: FAIL — `cfg.FocusLastSession` / `cfg.RememberGroupState` undefined (build error).

- [ ] **Step 3: Add the fields**

In `internal/config/config.go`, add to the `Config` struct (after `Editor`, line 38):

```go
	FocusLastSession   bool        `yaml:"focus_last_session"`   // re-select the last focused session on startup (default false)
	RememberGroupState bool        `yaml:"remember_group_state"` // restore manually-toggled group open/closed state on startup (default false)
```

No change to `DefaultConfig()` is needed — `false` is the zero value, and bools have no invalid range so `sanitizeConfig` needs no handling.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (all config tests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add focus_last_session and remember_group_state flags"
```

---

### Task 2: ViewStateStore

**Files:**
- Create: `internal/session/viewstate.go`
- Test: `internal/session/viewstate_test.go`

**Interfaces:**
- Produces:
  - `type ViewState struct { LastSessionID string; CollapsedGroups map[string]bool }` (json `last_session_id`, `collapsed_groups`)
  - `func NewViewStateStore(path string) *ViewStateStore` (empty path -> default; loads on construction)
  - `func DefaultViewStatePath() string` -> `~/.config/naviclaude/view-state.json`
  - `func (s *ViewStateStore) Get() ViewState` (returns a deep copy)
  - `func (s *ViewStateStore) Save(vs ViewState) error`
  - `func (s *ViewStateStore) LoadErr() error`

- [ ] **Step 1: Write the failing tests**

Create `internal/session/viewstate_test.go`:

```go
package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestViewStateSaveAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "view-state.json")
	s := NewViewStateStore(path)

	want := ViewState{
		LastSessionID:   "uuid-123",
		CollapsedGroups: map[string]bool{"hermes": true, "default": false},
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// A fresh store reading the same file must see the persisted values.
	got := NewViewStateStore(path).Get()
	if got.LastSessionID != "uuid-123" {
		t.Errorf("LastSessionID = %q, want %q", got.LastSessionID, "uuid-123")
	}
	if got.CollapsedGroups["hermes"] != true || got.CollapsedGroups["default"] != false {
		t.Errorf("CollapsedGroups = %v, want hermes:true default:false", got.CollapsedGroups)
	}
}

func TestViewStateMissingFile(t *testing.T) {
	s := NewViewStateStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	got := s.Get()
	if got.LastSessionID != "" || len(got.CollapsedGroups) != 0 {
		t.Errorf("missing file should yield zero ViewState, got %+v", got)
	}
	if s.LoadErr() != nil {
		t.Errorf("missing file should not be an error, got %v", s.LoadErr())
	}
}

func TestViewStateCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "view-state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewViewStateStore(path)
	if s.LoadErr() == nil {
		t.Error("corrupt file should surface via LoadErr")
	}
}

func TestViewStateGetReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	s := NewViewStateStore(filepath.Join(dir, "view-state.json"))
	if err := s.Save(ViewState{CollapsedGroups: map[string]bool{"a": true}}); err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	got.CollapsedGroups["a"] = false // mutate the copy
	if s.Get().CollapsedGroups["a"] != true {
		t.Error("Get() must return a copy; internal state was mutated")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/ -run ViewState -v`
Expected: FAIL — `ViewState` / `NewViewStateStore` undefined (build error).

- [ ] **Step 3: Write the implementation**

Create `internal/session/viewstate.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/session/ -run ViewState -v`
Expected: PASS (all four ViewState tests).

- [ ] **Step 5: Commit**

```bash
git add internal/session/viewstate.go internal/session/viewstate_test.go
git commit -m "feat(session): add ViewStateStore for persisted UI state"
```

---

### Task 3: Sidebar group-state export/seed

**Files:**
- Modify: `internal/ui/sidebar.go` (add two methods near `ExpandGroup`, ~line 545)
- Test: `internal/ui/sidebar_viewstate_test.go` (new)

**Interfaces:**
- Consumes: sidebar internals `collapsed map[string]bool`, `userToggled map[string]bool`.
- Produces:
  - `func (m *SidebarModel) ToggledGroups() map[string]bool` — `{groupName: collapsed}` for every group in `userToggled`.
  - `func (m *SidebarModel) SeedToggledGroups(groups map[string]bool)` — seed `userToggled` + `collapsed` before the first `SetSessions`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/sidebar_viewstate_test.go`:

```go
package ui

import (
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

func twoGroupSidebar() SidebarModel {
	m := NewSidebar(30, 12)
	m.SetSessions([]*session.Session{
		{ID: "a1", TmuxSession: "hermes", TmuxTarget: "hermes:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now()},
		{ID: "b1", TmuxSession: "default", TmuxTarget: "default:1.0", ProjectName: "p2", Status: session.StatusActive, LastActivity: time.Now()},
	})
	m.SetSize(29, 11)
	return m
}

func TestSeedToggledGroupsCollapsesGroup(t *testing.T) {
	m := NewSidebar(30, 12)
	m.SeedToggledGroups(map[string]bool{"hermes": true})
	m.SetSessions([]*session.Session{
		{ID: "a1", TmuxSession: "hermes", TmuxTarget: "hermes:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now()},
	})
	m.SetSize(29, 11)
	// A collapsed group hides its session rows: only the group header is a flat item.
	for _, it := range m.FlatItems() {
		if !it.IsGroup && it.Session != nil && it.Session.ID == "a1" {
			t.Fatal("seeded-collapsed group should hide its session row")
		}
	}
}

func TestSeedToggledGroupsSurvivesAutoCollapse(t *testing.T) {
	m := NewSidebar(30, 12)
	m.SetCollapseAfterHours(1) // aggressive auto-collapse
	m.SeedToggledGroups(map[string]bool{"hermes": false})
	m.SetSessions([]*session.Session{
		// Stale activity would normally auto-collapse the group.
		{ID: "a1", TmuxSession: "hermes", TmuxTarget: "hermes:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now().Add(-48 * time.Hour)},
	})
	m.SetSize(29, 11)
	// Seeded as expanded (and thus user-toggled), so auto-collapse must not hide it.
	found := false
	for _, it := range m.FlatItems() {
		if !it.IsGroup && it.Session != nil && it.Session.ID == "a1" {
			found = true
		}
	}
	if !found {
		t.Fatal("seeded-expanded group must survive auto-collapse")
	}
}

func TestToggledGroupsReflectsManualToggle(t *testing.T) {
	m := twoGroupSidebar()
	// ExpandGroup marks a group user-toggled; first collapse it via seed so the
	// expand is a real state change.
	m.SeedToggledGroups(map[string]bool{"hermes": true})
	m.SetSessions([]*session.Session{
		{ID: "a1", TmuxSession: "hermes", TmuxTarget: "hermes:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now()},
	})
	m.ExpandGroup("hermes")
	got := m.ToggledGroups()
	if v, ok := got["hermes"]; !ok || v != false {
		t.Errorf("ToggledGroups()[hermes] = (%v,%v), want (false,true)", v, ok)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run "SeedToggledGroups|ToggledGroups" -v`
Expected: FAIL — `SeedToggledGroups` / `ToggledGroups` undefined (build error).

- [ ] **Step 3: Add the two methods**

In `internal/ui/sidebar.go`, immediately after `ExpandGroup` (which ends near line 554), add:

```go
// ToggledGroups returns the collapsed state of every group the user has manually
// toggled, keyed by group name. Used to persist group open/closed state.
func (m *SidebarModel) ToggledGroups() map[string]bool {
	out := make(map[string]bool, len(m.userToggled))
	for name := range m.userToggled {
		out[name] = m.collapsed[name]
	}
	return out
}

// SeedToggledGroups pre-loads persisted group state so restored groups are
// treated exactly as if the user had toggled them: they render at the seeded
// collapsed value and are exempt from auto-collapse. Must be called before the
// first SetSessions so rebuildGroups honors the seeded state. Entries for groups
// that no longer exist are harmless -- rebuildGroups only iterates live groups.
func (m *SidebarModel) SeedToggledGroups(groups map[string]bool) {
	for name, collapsed := range groups {
		m.userToggled[name] = true
		m.collapsed[name] = collapsed
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run "SeedToggledGroups|ToggledGroups" -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Run the full ui package to check for regressions**

Run: `go test ./internal/ui/`
Expected: PASS (ok).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/sidebar.go internal/ui/sidebar_viewstate_test.go
git commit -m "feat(ui): add sidebar group-state export/seed methods"
```

---

### Task 4: App wiring (store, seed, restore, persist)

**Files:**
- Modify: `internal/app/app.go` — `Model` struct (~line 172-190), `New()` (~line 206-278), `activeSessionsMsg` handler (~line 423-448), `historySessionsMsg` handler (~line 450-479), `handleListKey` quit + nav (~line 858-860, ~971), `handleChangedFilesKey` quit (~line 1018-1021), global Ctrl+C (~line 813-815), `jumpToPane` (~line 1338-1348), `resumeMsg` jump path (~line 547-556)
- Test: `internal/app/viewstate_test.go` (new)

**Interfaces:**
- Consumes: `session.NewViewStateStore`, `session.ViewState`, `(*ViewStateStore).Get/Save`, `sidebar.SelectByID`, `sidebar.SeedToggledGroups`, `sidebar.ToggledGroups`, `m.selectPreviewSession`, `m.reloadMetricsForSelection`.
- Produces (unexported, same package): `Model.viewState *session.ViewStateStore`, `Model.restoredLastSession bool`, `func (m *Model) maybeRestoreLastSession() tea.Cmd`, `func (m Model) persistViewState()`.

- [ ] **Step 1: Add struct fields**

In `internal/app/app.go`, in the `Model` struct's Configuration block (after `refreshInterval`, ~line 189), add:

```go
	viewState           *session.ViewStateStore // persisted last-focused session + group state
	restoredLastSession bool                     // one-shot guard: restore the focused session only once at startup
```

- [ ] **Step 2: Construct the store and seed group state in `New()`**

In `New()`, add the store to the struct literal (in the `// Session aliases` / stores area, after `aliasStore: session.NewAliasStore(""),`, ~line 233):

```go
		// View state (last focused session + group open/closed state)
		viewState: session.NewViewStateStore(""),
```

Then, after the existing sort-order wiring (after `m.sidebar.SetSessionSortOrder(cfg.SessionSortOrder)`, ~line 261), add:

```go
	// Seed persisted group open/closed state before the first SetSessions so
	// auto-collapse honors the user's remembered toggles.
	if cfg.RememberGroupState {
		m.sidebar.SeedToggledGroups(m.viewState.Get().CollapsedGroups)
	}
```

- [ ] **Step 3: Add the restore and persist helpers**

At the end of `internal/app/app.go` (after the last function), add:

```go
// maybeRestoreLastSession re-selects the session focused when the app last
// closed, if focus_last_session is enabled and it has not already run. It is
// safe to call repeatedly: it does nothing once the session is restored or the
// user has navigated. Returns a metrics-load Cmd for the restored session, or
// nil. If the stored session is not present, the default first-session cursor is
// left untouched (the historical fallback behavior).
func (m *Model) maybeRestoreLastSession() tea.Cmd {
	if m.restoredLastSession || !m.cfg.FocusLastSession || m.viewState == nil {
		return nil
	}
	id := m.viewState.Get().LastSessionID
	if id == "" {
		return nil
	}
	if !m.sidebar.SelectByID(id) {
		// Not present yet; a later scan (history) may still contain it.
		return nil
	}
	m.restoredLastSession = true
	sel := m.sidebar.SelectedSession()
	if sel == nil {
		return nil
	}
	m.selectPreviewSession(sel)
	return m.reloadMetricsForSelection(sel)
}

// persistViewState writes the current focused session and manually-toggled group
// state to disk. Called at quit. The full state is always written; each field is
// applied on startup only if its config toggle is on.
func (m Model) persistViewState() {
	if m.viewState == nil {
		return
	}
	vs := session.ViewState{CollapsedGroups: m.sidebar.ToggledGroups()}
	if sel := m.sidebar.SelectedSession(); sel != nil {
		vs.LastSessionID = sel.ID
	}
	_ = m.viewState.Save(vs)
}
```

- [ ] **Step 4: Call restore from the session-load handlers**

In the `activeSessionsMsg` handler, replace the selected-session block (currently ~lines 438-446):

```go
			if sel := m.sidebar.SelectedSession(); sel != nil {
				m.preview.SetSession(sel)
				// Fire metrics load for the initially selected session.
				if sel.ID != "" && sel.ID != m.metricsSessionID {
					m.metricsSessionID = sel.ID
					return m, tea.Batch(m.refreshHistoryCmd(), loadMetricsCmd(sel))
				}
			}
```

with:

```go
			// Restore the last focused session if enabled (may be active).
			restoreCmd := m.maybeRestoreLastSession()
			if sel := m.sidebar.SelectedSession(); sel != nil {
				m.preview.SetSession(sel)
				// Fire metrics load for the initially selected session.
				if sel.ID != "" && sel.ID != m.metricsSessionID {
					m.metricsSessionID = sel.ID
					return m, tea.Batch(m.refreshHistoryCmd(), loadMetricsCmd(sel), restoreCmd)
				}
			}
			if restoreCmd != nil {
				return m, tea.Batch(m.refreshHistoryCmd(), restoreCmd)
			}
```

In the `historySessionsMsg` handler, replace the trailing selected-session block (currently ~lines 475-477):

```go
			if sel := m.sidebar.SelectedSession(); sel != nil {
				m.preview.SetSession(sel)
			}
```

with:

```go
			// Restore the last focused session if enabled (may be a closed
			// session that only appears after the history scan). After history has
			// loaded, all sessions are known: if the stored session is still
			// absent, the default first-session cursor stands (the fallback).
			restoreCmd := m.maybeRestoreLastSession()
			m.restoredLastSession = true
			if sel := m.sidebar.SelectedSession(); sel != nil {
				m.preview.SetSession(sel)
			}
			if restoreCmd != nil {
				return m, restoreCmd
			}
```

- [ ] **Step 5: Trip the one-shot guard on user navigation**

In `handleListKey`, at the top of the `default:` branch (the navigation delegate, ~line 972, right after `key := ...` is already out of scope — the branch begins `var cmd tea.Cmd`), add as the first line of the `default:` case:

```go
		// Once the user navigates, never auto-restore (avoids yanking the cursor
		// if a late history scan resolves the stored session).
		m.restoredLastSession = true
```

- [ ] **Step 6: Persist at the quit choke points**

Add `m.persistViewState()` immediately before `m.restorePreviewedPane()` at each user-initiated quit:

1. `handleListKey`, `case m.keys.Quit:` (~line 858):
```go
	case m.keys.Quit:
		m.persistViewState()
		m.restorePreviewedPane()
		return m, tea.Quit
```

2. `handleChangedFilesKey`, `case m.keys.Quit:` (~line 1018):
```go
	case m.keys.Quit:
		// The quit key still quits from the focused panel (matches list mode).
		m.persistViewState()
		m.restorePreviewedPane()
		return m, tea.Quit
```

3. Global Ctrl+C in `handleKey` (~line 813):
```go
	if key == KeyCtrlC && m.mode != ModePassthrough {
		m.persistViewState()
		m.restorePreviewedPane()
		return m, tea.Quit
	}
```

4. `jumpToPane` (~line 1343):
```go
	m.persistViewState()
	m.restorePreviewedPane()
	// Switch tmux client to the session's pane.
```

5. `resumeMsg` jump path in `Update` (~line 550):
```go
		if m.pendingResumeJump {
			// f-style resume: jump to the resumed pane ...
			m.pendingResumeJump = false
			m.persistViewState()
			m.restorePreviewedPane()
			_ = m.tmuxClient.SwitchClient(msg.tmuxTarget)
			_ = m.tmuxClient.SelectPane(msg.tmuxTarget)
			return m, tea.Quit
		}
```

- [ ] **Step 7: Verify it builds**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 8: Write the app-level restore test**

Create `internal/app/viewstate_test.go`:

```go
package app

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// newTestModel builds a minimal Model with a sidebar and a temp-backed view
// store, bypassing New() (which touches tmux and the real config path).
func newTestModel(t *testing.T, cfg config.Config, stored session.ViewState) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "view-state.json")
	store := session.NewViewStateStore(path)
	if err := store.Save(stored); err != nil {
		t.Fatal(err)
	}
	m := Model{
		sidebar:   ui.NewSidebar(30, 24),
		preview:   ui.NewPreview(50, 24),
		cfg:       cfg,
		viewState: store,
	}
	return m
}

func sessions() []*session.Session {
	return []*session.Session{
		{ID: "first", TmuxSession: "aaa", TmuxTarget: "aaa:1.0", ProjectName: "p1", Status: session.StatusActive, LastActivity: time.Now()},
		{ID: "second", TmuxSession: "bbb", TmuxTarget: "bbb:1.0", ProjectName: "p2", Status: session.StatusActive, LastActivity: time.Now()},
	}
}

func TestRestoreSelectsStoredSession(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.FocusLastSession = true
	m := newTestModel(t, cfg, session.ViewState{LastSessionID: "second"})
	m.sidebar.SetSessions(sessions())

	_ = m.maybeRestoreLastSession()

	sel := m.sidebar.SelectedSession()
	if sel == nil || sel.ID != "second" {
		t.Fatalf("restore should select stored session; got %v", sel)
	}
}

func TestRestoreFallsBackWhenSessionGone(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.FocusLastSession = true
	m := newTestModel(t, cfg, session.ViewState{LastSessionID: "ghost"})
	m.sidebar.SetSessions(sessions())

	_ = m.maybeRestoreLastSession()

	sel := m.sidebar.SelectedSession()
	if sel == nil || sel.ID != "first" {
		t.Fatalf("missing stored session should leave first-session cursor; got %v", sel)
	}
	if m.restoredLastSession {
		t.Error("guard must stay false when nothing was restored, so a later scan can retry")
	}
}

func TestRestoreDisabledByConfig(t *testing.T) {
	cfg := config.DefaultConfig() // FocusLastSession = false
	m := newTestModel(t, cfg, session.ViewState{LastSessionID: "second"})
	m.sidebar.SetSessions(sessions())

	_ = m.maybeRestoreLastSession()

	sel := m.sidebar.SelectedSession()
	if sel == nil || sel.ID != "first" {
		t.Fatalf("disabled restore should leave first-session cursor; got %v", sel)
	}
}

func TestPersistViewStateWritesSelection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.FocusLastSession = true
	m := newTestModel(t, cfg, session.ViewState{})
	m.sidebar.SetSessions(sessions())
	m.sidebar.SelectByID("second")

	m.persistViewState()

	if got := m.viewState.Get().LastSessionID; got != "second" {
		t.Fatalf("persisted LastSessionID = %q, want %q", got, "second")
	}
}

var _ tea.Cmd // keep the bubbletea import if unused above
```

Note: if `go vet` flags the unused `tea` import, delete the `import` line for `tea` and the `var _ tea.Cmd` line. Keep them only if a compile error demands the import.

- [ ] **Step 9: Run the app tests**

Run: `go test ./internal/app/ -run "Restore|PersistViewState" -v`
Expected: PASS (all four tests).

- [ ] **Step 10: Run the full suite and build**

Run: `go build ./... && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 11: Commit**

```bash
git add internal/app/app.go internal/app/viewstate_test.go
git commit -m "feat(app): restore last focused session and group state on reopen"
```

---

## Self-Review

**Spec coverage:**
- Config flags `focus_last_session` + `remember_group_state`, default false -> Task 1.
- `view-state.json` store modeled on AliasStore, atomic save, missing/corrupt handling -> Task 2.
- Write-always/apply-per-toggle policy -> Task 4 `persistViewState` (always writes both) + `maybeRestoreLastSession`/`New()` seed (each gated by its flag).
- Sidebar `ToggledGroups` / `SeedToggledGroups`, seeded before first SetSessions, auto-collapse exemption -> Task 3 (+ test `TestSeedToggledGroupsSurvivesAutoCollapse`).
- Session restore in both activeSessionsMsg and historySessionsMsg, one-shot guard, guard tripped on user nav, fallback to first session -> Task 4 steps 4-5 (+ tests).
- Save at all five quit choke points -> Task 4 step 6.
- Not restored (passthrough, scroll, search, right panel) -> nothing added for these; right panel `rightPanelOpen` stays default false; confirmed no code touches them.

**Placeholder scan:** No TBD/TODO; all code blocks complete; the one conditional note (unused `tea` import) has explicit instructions.

**Type consistency:** `ViewState{LastSessionID, CollapsedGroups}`, `NewViewStateStore`, `Get`, `Save`, `LoadErr`, `ToggledGroups`, `SeedToggledGroups`, `maybeRestoreLastSession`, `persistViewState`, `restoredLastSession`, `viewState` — used identically across Tasks 2-4. `SelectByID` returns bool (matches existing signature). `reloadMetricsForSelection(sel) tea.Cmd` and `selectPreviewSession(sel)` match existing app methods.
