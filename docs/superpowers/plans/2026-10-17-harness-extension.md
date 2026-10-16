# Harness Extension Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move every Claude Code-specific behavior behind an in-process `harness.Harness` interface with Claude as the only implementation, with zero user-visible behavior change.

**Architecture:** `internal/session` becomes a generic leaf (types, process tree, status tracker, tmux lifecycle plumbing). `internal/harness` holds the interface, optional capability interfaces, a registry and the generic pane Detector. `internal/harness/claude` holds all `~/.claude` parsing, pane-text classification, resume/fork commands and stats computation. `app` wires `claude.New` into a registry once; `ui` never imports `harness`.

**Tech Stack:** Go 1.26, Bubble Tea, tmux CLI.

**Spec:** `docs/superpowers/specs/2026-10-17-harness-extension-design.md`

## Global Constraints

- NO git commits. Every "commit" step is replaced by: verify green; no commit (user decides).
- Behavior must be identical: same config keys/defaults (`claude_command`, `process_names`, `active_window_secs`, `cpu_active_threshold`), same window names, same resume command `cd '<cwd>' && claude --resume "<id>"` (+ ` --fork-session`), same alias/view-state files.
- Baseline `go test -v ./... | grep -c -- '--- PASS'` = 597, 0 FAIL. Count must not drop; moved tests keep assertions unchanged.
- `ui` imports only `session`, `stats`, `styles` (never `harness`). `app` imports `harness/claude` only in `New`.
- Harness implementations must be goroutine-safe.
- Comments: 1-2 lines max, no change narration.
- Build the dashboard binary only in the final task: `go build -o ./naviclaude ./cmd/naviclaude`.

## Review Focus

1. Closed sessions must carry `Harness="claude"` (stamped by history scan) or `Registry.For` must resolve empty to default — otherwise Resume/Fork/conversation silently vanish for closed sessions.
2. Live sessions must get `SessionFile` from `Enrich`; detail popup, metrics, changed-files and their mtime refresh read it instead of a Claude path helper.
3. Concurrent `Detect` goroutines: model/last-activity/AI-title caches move into the Claude harness with their mutexes (`-race` must pass).
4. Subprocess panes (Claude inside nvim) must still skip content classification and show the "running inside" message.
5. Native-status short-circuit must still skip pane capture when native status exists (performance + no false WAITING).

---

### Task 1: harness package skeleton + Session.Harness/Key

**Files:**
- Modify: `internal/session/session.go` (add `Harness string` field, `Key()` method)
- Create: `internal/session/session_key_test.go`
- Create: `internal/harness/harness.go` (Info, Harness, capability interfaces, `As*` helpers)
- Create: `internal/harness/registry.go`
- Create: `internal/harness/fake_test.go`, `internal/harness/registry_test.go`

**Interfaces (Produces):**
```go
// session
func (s *Session) Key() string // ID when Harness is "" or "claude", else Harness+":"+ID

// harness
type Info struct{ Name, Label, AssistantLabel string; PromptCacheTTL time.Duration }
type Harness interface {
    Info() Info
    MatchProcess(name string) bool
    Enrich(s *session.Session)
    NativeStatus(s *session.Session) (session.SessionStatus, bool)
    ClassifyPane(content string) session.PaneSignal
    RecentlyActive(s *session.Session, window time.Duration) bool
    StartCommand() string
}
type Resumer interface{ ResumeCommand(s *session.Session) string }
type Forker interface{ ForkCommand(s *session.Session) string }
type HistoryProvider interface {
    ScanClosedAndAll(closedHours float64, activeIDs map[string]bool) (closed, all []*session.Session, err error)
    Summaries() (map[string]string, error)
}
type ConversationProvider interface{ LoadConversation(s *session.Session, maxTurns int) ([]session.ConversationEntry, error) }
type MetricsProvider interface{ LoadMetrics(s *session.Session) (*session.SessionMetrics, error) }
type ChangedFilesProvider interface {
    LoadChangedFiles(s *session.Session) ([]session.ChangedFile, error)
    ChangeStamp(s *session.Session) time.Time
}
type StatsProvider interface {
    StatsKey() int
    ComputeStats(activeCount int, filter string) (*stats.Stats, error)
}
func AsResumer(h Harness) (Resumer, bool) // + AsForker, AsHistory, AsConversation, AsMetrics, AsChangedFiles, AsStats

type Registry struct{ /* ordered */ }
func NewRegistry(hs ...Harness) *Registry
func (r *Registry) All() []Harness
func (r *Registry) Get(name string) (Harness, bool)
func (r *Registry) For(s *session.Session) (Harness, bool) // "" → Default()
func (r *Registry) Default() Harness
```

- [ ] Step 1: Write tests: `Key()` table (claude/""/other harness, empty ID); registry order, `Get`, `For` with empty Harness → default, unknown → false; `As*` true for a full fake, false for a core-only fake.
- [ ] Step 2: `go test ./internal/session/ ./internal/harness/` → FAIL (undefined).
- [ ] Step 3: Implement.
- [ ] Step 4: `go test ./...` → PASS; pass count ≥ 597 + new.
- [ ] Step 5: verify green; no commit.

### Task 2: create harness/claude and move pure Claude parsers

Move (with their tests, `package claude`, assertions unchanged; use `git mv` so history follows):
`session/classify.go`, `aititle.go`, `lastactivity.go`, `title.go`, `history.go`, `conversation.go` (parsing parts), `metrics.go` (parsing + `ContextLimitForModel`), `changedfiles.go` (transcript parsing; `lineDiff`/LCS) and their `_test.go` files; the `~/.claude/sessions/<pid>.json` metadata readers (`sessionMetadata`, `readSessionMetadata*`, `mapNativeStatus`, `nativeStatusString`, `NativeStatus`), `extractSessionIDFallback`, `fullCommandLine`, `parseResumeFlag`, `findLatestSessionFile`, `sessionFileModTime`, `extractModelFromSessionFile`, `maxTranscriptLineBytes` from `detector.go` into `claude/metadata.go`.

Stay in `session` (generic types): `PaneSignal` + `Signal*` consts (move type out of classify.go into `session/signal.go`), `ConversationEntry`, `SessionMetrics`, `ChangedFile`, `DiffStat`, `git.go`, status tracker, proc tree.

- [ ] Step 1: Move files; fix package names, exported references (`session.Session`, `session.StatusActive`, ...). Tests that only touch moved code move too; split mixed test files (e.g. `fixes_extra_test.go`, `fixes_owned_test.go`, `concurrency_test.go`, `native_status_test.go`, `detector_test.go`) by what they exercise.
- [ ] Step 2: `go build ./... && go test ./internal/session/ ./internal/harness/...` → PASS (detector.go temporarily calls `claude`? No — cycle. Instead Task 2 and 3 land together: do not run app tests until Task 3 completes.)
- [ ] Step 3: verify green after Task 3; no commit.

### Task 3: generic Detector in harness; Claude implements core Harness

**Files:**
- Create: `internal/harness/detector.go` (generic loop from `session/detector.go`: `Detect`, `sessionFromPane` ownership loop, base-session build, status arbitration, tracker prune)
- Keep in `session`: `ProcessTree`, `BuildProcessTree`, `Stats`, `SubtreeCPU`; export `ParentAppName`, `IsAncestorOf`, `FindMatchingDescendant`, `Name(pid)`
- Create: `internal/harness/claude/claude.go` (`Options{Command string; ProcessNames []string}`, `New(Options) *Harness`, core methods, caches + mutexes moved from Detector)
- Delete: `internal/session/detector.go` (after split), `internal/preview/status.go`
- Tests: generic detector tests with fakeHarness in `harness/detector_test.go`; Claude enrich/status tests in `claude/`

**Interfaces:**
```go
func NewDetector(tc *tmux.Client, reg *Registry, activeWindowSecs int, cpuThreshold float64) *Detector // defaults 5s / 5%
func (d *Detector) Detect() ([]*session.Session, error)
// ResolvePreviewStatus is the preview path's NativeStatus→ClassifyPane order.
func ResolvePreviewStatus(h Harness, s *session.Session, content string) session.SessionStatus

// claude
func New(o Options) *Harness // Command "" → "claude"; ProcessNames empty → ["claude"]
```
Claude `Enrich` = old `buildSession` minus tmux/process fields: ID via per-PID metadata → fallback; DisplayName via `resolveTitle`; Model, LastActivity, AI title via caches; `SessionFile = SessionFilePath(ID, CWD)` when ID known. Native status read inside `NativeStatus(s)` (per-PID then by-ID fallback — same as old `nativeStatusString(pid, id)`). `ClassifyPane` = `ClassifyPaneContent`. `RecentlyActive` = transcript mtime within window. `MatchProcess` = configured names (version-title already handled because the Detector also checks the ps basename).

Detector testability: the Detector takes a `paneSource` interface (`ListPanes`, `CapturePaneOutput`) satisfied by `*tmux.Client`, and a `buildTree func() *session.ProcessTree` defaulting to `session.BuildProcessTree`, so fakes can drive it.

- [ ] Step 1: Write `harness/detector_test.go`: first-matching harness owns pane; non-matching pane ignored; native status short-circuits (fake `CapturePaneOutput` must not be called); fallback uses tracker with ClassifyPane=SignalWaiting → StatusWaiting; subprocess pane does not capture; `Harness` stamped.
- [ ] Step 2: run → FAIL.
- [ ] Step 3: implement split; update `app` construction temporarily to `harness.NewDetector(tc, harness.NewRegistry(claude.New(...)), ...)` and preview path to `harness.ResolvePreviewStatus`.
- [ ] Step 4: `go vet ./... && go test -race ./...` → PASS.
- [ ] Step 5: verify green; no commit.

### Task 4: Manager takes harness commands; Claude Resumer/Forker

**Files:** `internal/session/manager.go`, `manager_test.go`, `internal/harness/claude/commands.go` (+test), `internal/app/app.go` call sites.

**Interfaces:**
```go
func (m *Manager) Resume(sess *Session, targetTmuxSession, command string) error            // runs "cd '<cwd>' && <command>"
func (m *Manager) ResumeInSession(sess *Session, targetName, command string) (string, error)
func (m *Manager) CreateNewTmuxSession(cwd, command, sessionName, fallbackName string) (tmuxSession, target string, err error)
func (m *Manager) CreateNewWithTarget(cwd, targetTmuxSession, command, fallbackName string) (string, error)
// resumeWindowName: ProjectName, else sess.Harness, else "claude"
// claude
func (h *Harness) ResumeCommand(s *session.Session) string // `claude --resume "<id>"`
func (h *Harness) ForkCommand(s *session.Session) string   // ResumeCommand + " --fork-session"
```
`ForkResume` is removed; app calls `Resume(sess, target, forker.ForkCommand(sess))`. Empty `command` in `CreateNewWithTarget` returns an error (the claude harness never returns empty).

- [ ] Step 1: Update manager tests to pass explicit commands and assert the same final strings; add claude command tests (`claude --resume "abc"`, fork suffix).
- [ ] Step 2: run → FAIL. Step 3: implement + update app call sites via `m.registry.For(sess)` + `harness.AsResumer/AsForker`. Step 4: `go test -race ./...` PASS. Step 5: verify green; no commit.

### Task 5: stats compute into Claude StatsProvider

**Files:** move `internal/stats/compute.go` (+ `compute_test.go`, Claude-only parts of `fixes_test.go`/`fixes_extra_test.go`/`concurrency_test.go`/`realcache_test.go` as they exercise Compute) into `internal/harness/claude/stats.go`; keep `stats/stats.go`, `stats/cache.go` and cache tests. Move `statsCacheKey` from `app/handle_stats.go` into `claude.StatsKey()`.

- [ ] Steps: move; `ComputeStats(activeCount, filter)` calls the moved `Compute("", activeCount, filter)`; app uses first `AsStats` provider (none → stats overlay shows nothing / key ignored). `go test -race ./...` PASS. verify green; no commit.

### Task 6: app rewired onto registry; identity via Key()

**Files:** `internal/app/app.go`, `handle_metrics.go`, `handle_detail.go`, `handle_changedfiles.go`, `handle_stats.go`, tests.

- `Model.registry *harness.Registry`, `Model.detector *harness.Detector`; drop `historyScanner`, `statusDetector`.
- `refresh*Cmd`: for each `AsHistory` provider, `ScanClosedAndAll(closedHours, activeIDsFor(active, name))` and concatenate; summaries merged from all providers, matched by harness name + ID.
- Loaders: `loadMetricsCmd`/`loadDetailDataCmd` → `AsMetrics(h).LoadMetrics(sess)`; changed files → `AsChangedFiles`; mtime gate → `ChangeStamp`; closed preview → `AsConversation`. Absent capability → empty msg.
- Aliases `applyAliases`, rename (`renameSessionID`), view state `LastSessionID`, `SelectByID` use `Key()`; sidebar `SelectByID` compares `Key()`.
- [ ] Step 1: add `app` test with a core-only fake registry: `loadMetricsCmd` returns `metricsMsg{metrics:nil}`, conversation returns empty content, no panic.
- [ ] Steps 2-4: FAIL → implement → `go test -race ./...` PASS. Step 5: verify green; no commit.

### Task 7: UI literals from Info; capability gating

**Files:** `internal/app/app.go`, `internal/ui/contextmenu.go`, `internal/ui/preview.go`, `internal/ui/detail.go`, tests.

- Conversation labels: `Info.AssistantLabel`; subprocess message: `Info.Label`; dir picker title: `"New " + Default().Info().Label + " session directory"`; placeholders `ProjectName: Default().Info().Name`.
- `ContextMenuModel.Show(x, y int, s *session.Session, caps MenuCaps)` with `type MenuCaps struct{ CanResume, CanFork bool }`; closed menu omits Resume/Fork when false.
- `PreviewModel.SetCacheTTLs(map[string]time.Duration)` (key = harness name, "" = default); `cacheExpired(s, now, ttl)`; ttl 0 → false.
- `ui/detail.go` reads `s.SessionFile` only.
- [ ] Tests: contextmenu caps table (claude caps → identical items as today; no caps → no Resume/Fork); cacheExpired with ttl 0. FAIL → implement → PASS. verify green; no commit.

### Task 8: final verification + local build

- [ ] `grep -rn '\.claude\|"claude\|Claude' internal --include='*.go' | grep -v '^internal/harness/claude/' | grep -v _test` → only branding/comments; fix stragglers.
- [ ] `go vet ./...`, `go test -race ./...`, `go test -v ./... | grep -c -- '--- PASS'` ≥ 597 and 0 FAIL.
- [ ] `go build -o ./naviclaude ./cmd/naviclaude`.
- [ ] Hand to user for smoke test; no commit.
