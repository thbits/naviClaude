# Harness extension: Claude Code behind an in-process adapter

Date: 2026-10-17
Status: Implemented (uncommitted), pending user smoke test

## Problem

naviClaude's tmux, preview, process-tree, git, alias and view-state mechanics are
agent-neutral, but session detection, status, history, transcripts, metrics,
changed files, stats, lifecycle commands and several UI strings are hard-wired to
Claude Code (`~/.claude`, `claude --resume`, Claude's pane UI text). Adding any
other agent CLI ("harness") today means editing all of those call sites.

## Goal

Move every Claude Code-specific behavior behind a `Harness` interface, with Claude
as the first and only built-in implementation, **with no user-visible behavior
change**. Long term, naviClaude should observe many harnesses (herdr/cmux-style)
and notify on status changes; this refactor is the foundation for that.

## Non-goals

- No second harness (Codex, Pi, OMP, ...) in this change.
- No notifications in this change (separate spec). The design only guarantees that
  every harness reports status in the shared `session.SessionStatus` vocabulary so
  a future notifier can watch transitions harness-agnostically.
- No external plugin protocol or dynamically loaded plugins: harnesses are
  in-process Go packages registered at startup.
- No config file format change. No per-harness `harnesses:` config block yet.
- No unrelated cleanup (e.g. unused `ResumeInCurrent`, `Model.processNames`).

## Package layout

```
internal/session          generic leaf (imports tmux only)
  Session, SessionStatus, PaneSignal, ProcessTree (+SubtreeCPU, subprocess
  detection, descendant search), StatusTracker, git diff stats, AliasStore,
  ViewStateStore, Manager (tmux plumbing), shared data types:
  ConversationEntry, SessionMetrics, ChangedFile, DiffStat

internal/stats            generic types + on-disk cache only: Stats,
                          ModelUsageEntry, Cache

internal/harness          (imports session, stats, tmux)
  Harness + optional capability interfaces, Info, Registry, capability
  helpers, generic Detector (moved from session/detector.go)

internal/harness/claude   (imports harness, session, stats, tmux)
  all Claude specifics: process names + version-title matching,
  ~/.claude/sessions/<pid>.json metadata and native status, session-ID
  fallback (--resume flag, newest transcript), pane-text classifier,
  history scanner + history.jsonl summaries, conversation, metrics and
  context limits, changed-files transcript parsing, AI title, last
  activity, model families, cwd slug, resume/fork commands, stats Compute
```

Rules:
- `app` imports `harness/claude` in exactly one place: registry construction.
- `ui` imports only `session`, `stats`, `styles`; never `harness` or `claude`.
  It learns capabilities through plain values the app passes in.
- `preview/status.go` is deleted; status classification is the harness's job.
- `Session` gains `Harness string` (`"claude"` today). The private
  `nativeStatus` / `nativeStatusAt` fields are removed; the Claude harness reads
  and consumes native status inside its own calls.
- Fields that are Claude-sourced but conceptually portable (ID, Slug, Model,
  Version, Summary, SessionFile, GitBranch) stay on `Session` and are optional
  per harness; the UI already tolerates empty values.

## Interfaces

Principle: harnesses supply facts and signals; generic code makes decisions.

```go
package harness

type Info struct {
    Name           string        // stable key, "claude"
    Label          string        // "Claude"
    AssistantLabel string        // conversation role label, "Claude"
    PromptCacheTTL time.Duration // 1h for Claude; 0 hides the cache-expired chip
}

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
type Forker  interface{ ForkCommand(s *session.Session) string }
type HistoryProvider interface {
    ScanClosedAndAll(closedHours float64, activeIDs map[string]bool) (closed, all []*session.Session, err error)
    Summaries() (map[string]string, error)
}
type ConversationProvider interface {
    LoadConversation(s *session.Session, maxTurns int) ([]session.ConversationEntry, error)
}
type MetricsProvider interface {
    LoadMetrics(s *session.Session) (*session.SessionMetrics, error)
}
type ChangedFilesProvider interface {
    LoadChangedFiles(s *session.Session) ([]session.ChangedFile, error)
    ChangeStamp(s *session.Session) time.Time
}
type StatsProvider interface {
    StatsKey() int
    ComputeStats(activeCount int, filter string) (*stats.Stats, error)
}
```

Semantics:
- `MatchProcess` receives a process basename or tmux `pane_current_command`.
  Claude matches its configured process names and its version-string process
  title, exactly as today.
- `Enrich` fills ID, DisplayName (per-PID name / AI title via `resolveTitle`),
  Model, LastActivity, SessionFile on a session whose tmux/process fields the
  Detector has already set. It must not set `Status`.
- `NativeStatus` returns `(status, true)` only when the harness has an
  authoritative status; Claude maps `busy|waiting|idle|shell` from
  `~/.claude/sessions/<pid>.json` with the existing per-PID then by-session-ID
  fallback.
- `activeIDs` passed to `ScanClosedAndAll` and the keys of `Summaries()` are
  that harness's own bare IDs; the app partitions by harness before calling.
- Capability helpers (`harness.AsResumer(h)`, `AsForker`, `AsHistory`, ...)
  wrap the type assertions.
- Implementations are called from concurrent goroutines (Bubble Tea runs
  overlapping `tea.Cmd`s) and must be goroutine-safe. Claude's per-session
  model / last-activity / AI-title caches and their mutexes move from the
  Detector into the Claude harness.
- Every `HistoryProvider` stamps `Harness = Info().Name` on the sessions it
  returns. `Enrich` sets `SessionFile` to the constructed transcript path when
  the ID is known, so `ui/detail.go` and the app read `s.SessionFile` instead
  of calling a Claude path helper; capability loaders keep today's
  verify-then-derive path semantics internally.

### Registry

`harness.NewRegistry(hs ...Harness)` keeps registration order.
- `All() []Harness`
- `Get(name string) (Harness, bool)`
- `For(s *session.Session) (Harness, bool)` by `s.Harness`; an empty
  `Harness` resolves to `Default()` (consistent with `Key()`).
- `Default() Harness` is the first registered harness (target for new sessions).

### Detector (generic)

`harness.NewDetector(tc, reg, activeWindowSecs, cpuThreshold)` with today's
defaults (5s, 5%). `Detect()`:
1. List panes; build the process tree once; skip popup panes and naviClaude's
   own pane (and its ancestors).
2. For each pane, for each harness in registry order: the pane matches if
   `MatchProcess(pane.CurrentCommand)` or `MatchProcess(tree name of pane PID)`,
   else the first matching descendant within depth 6. First harness wins.
3. Build the base session: TmuxSession, TmuxTarget, CWD, ProjectName, PID,
   CPU/Memory, Subprocess/SubprocessParent, `LastActivity = now`,
   `Status = Idle`, `Harness = Info().Name`. Then `h.Enrich(s)`.
4. Status: if `h.NativeStatus(s)` is ok, use it. Otherwise capture pane content
   (only when `TmuxTarget != ""` and not a subprocess) and feed
   `h.ClassifyPane(content)`, `SubtreeCPU >= threshold`, and
   `h.RecentlyActive(s, window)` into the shared `StatusTracker`.
5. Prune tracker entries for targets not seen this cycle.

The preview path resolves status the same way: `NativeStatus`, then
`ClassifyPane` on the captured content.

### Manager

`session.Manager` no longer builds `claude ...` strings. Its create/resume
functions take a harness-provided shell command; it keeps the `cd '<cwd>' &&`
prefix, quoting, send-keys and window naming. Claude's `ResumeCommand` is
`claude --resume "<id>"` and `ForkCommand` appends `--fork-session` (the
literal `claude` binary is kept for resume, as today; new sessions use the
configured `claude_command`). Window-name fallbacks use the harness name, which
is `"claude"` for Claude, so names are unchanged.

### Stats

`internal/stats` keeps `Stats`, `ModelUsageEntry` and `Cache`. `Compute`,
`CountSessionFiles` and the `~/.claude/projects` glob cache key move into the
Claude harness as `StatsProvider`. The app uses the first registered
`StatsProvider`; with none, the stats view is unavailable.

## Wiring, config, identity

```go
claudeH := claude.New(claude.Options{Command: cfg.ClaudeCommand, ProcessNames: cfg.ProcessNames})
reg := harness.NewRegistry(claudeH)
det := harness.NewDetector(tc, reg, cfg.ActiveWindowSecs, cfg.CPUActiveThreshold)
```

- `Model.detector` / `Model.historyScanner` become `Model.registry` /
  `Model.detector`. `handle_*.go` look up `m.registry.For(sess)` plus a
  capability helper. History refresh iterates all `HistoryProvider`s and
  concatenates; summary enrichment merges all providers' `Summaries()`.
- Config keys `claude_command`, `process_names`, `active_window_secs`,
  `cpu_active_threshold` keep names, defaults and sanitization.
- `Session.Key()` returns `ID` when `Harness == "claude"` (or empty), else
  `Harness + ":" + ID`. Aliases, view state (`last_session_id`) and the
  active/closed merge use `Key()`. Existing files keep working without migration.

UI text and gating:

| Today | After |
|---|---|
| "You" / "Claude" conversation labels | `Info.AssistantLabel` |
| "Claude is running inside X" / "...not Claude." | `Info.Label` |
| "New Claude session directory" | `Default().Info().Label` |
| placeholder `ProjectName: "claude"` | `Info.Name` |
| "Fork & Resume" menu item | shown only if harness is a `Forker` |
| "Resume" actions | only if harness is a `Resumer` |
| fixed 1h cache-expired chip | `Info.PromptCacheTTL`, 0 = none |
| conversation preview, metrics, changed files, stats | skipped/hidden when capability absent |

Branding (binary name, title bar, README) and the `opus` stats bar style are
unchanged.

## Error handling

Unchanged semantics: missing `~/.claude` files fall back exactly as today.
Missing capabilities fail closed: the feature is hidden or its loader returns
an empty result, never an error toast. Panes matching no harness are ignored.

## Testing

- Claude-specific tests move with their code into `internal/harness/claude`
  with assertions unchanged (only package/constructor changes); they are the
  regression proof.
- Generic tests stay in `session` (status tracker, process-tree CPU, git,
  aliases, view state, Manager with arbitrary commands).
- New `internal/harness` tests with a `fakeHarness`: detector ownership (first
  match, descendant walk, popup/self skip), status arbitration (native
  short-circuit, fallback via tracker), registry lookup/default, capability
  helpers, `Session.Key()`.
- `app` test with a core-only fake harness: no Fork in the context menu; metrics,
  conversation and changed-files loaders return quietly.
- Done when `go vet ./...`, `go test -race ./...` and `go build ./...` pass, a
  grep finds no `.claude`, `claude ` command strings or "Claude" UI literals
  outside `internal/harness/claude` (branding excepted), and a manual smoke run
  against live sessions shows identical sidebar, status, preview, resume, fork,
  stats, changed files and detail.

## Migration order

Each step compiles and passes tests:
1. `internal/harness` interfaces, Info, Registry, helpers; `Session.Harness`,
   `Session.Key()`; fake-harness tests.
2. Create `harness/claude`; move pure Claude parsers + tests (classify,
   metadata/native status, history, conversation, metrics, changed files,
   AI title, last activity, title).
3. Move Detector to `harness`, split generic vs Claude; delete
   `preview/status.go`.
4. `Manager` takes commands; resume/fork strings move to `claude`.
5. `stats.Compute` and cache key into `claude` as `StatsProvider`.
6. Rewire `app` onto registry and capability helpers; aliases, view state and
   merge use `Key()`.
7. UI literals from `Info`; capability gating in context menu and loaders.
8. Leftover grep, race tests, build, smoke run.
