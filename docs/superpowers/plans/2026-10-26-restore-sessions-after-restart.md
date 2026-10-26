# Restore Sessions After a Restart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** While naviClaude is open it records which agent conversation runs in which tmux pane; on the first launch after a tmux restart it resumes each one back in place automatically.

**Architecture:** A per-tmux-server snapshot in the user cache dir (`session.LiveStateStore`), written from the existing active-session refreshes once a startup restore check has finished. At startup a single `tea.Cmd` (`restorer.run`) compares the snapshot's server start time to the current server, waits for tmux-resurrect to settle, re-detects live sessions, builds a pure placement plan (same pane → same tmux session → recreate) and executes it through the session manager.

**Tech Stack:** Go, Bubble Tea, tmux CLI via `os/exec`.

**Spec:** `docs/superpowers/specs/2026-10-26-restore-sessions-after-restart-design.md`

## Global Constraints

- No git commits or pushes: the user commits on request only. Leave all changes in the working tree.
- Comments: 1–2 lines max, say the why, never narrate changes; one-line docstrings.
- Config key `restore_sessions`, field `RestoreSessions`, default `true`. When `false`, never read or write the snapshot.
- Snapshot path: `filepath.Join(os.UserCacheDir(), "naviclaude", "live-sessions.json")`, keyed by tmux `#{socket_path}`.
- Location stored as tmux session name + window index + pane index; never `%N`/`@N` ids.
- Resurrect settle: server up ≥ 15s, then two consecutive pane listings 2s apart that are identical; give up waiting after 60s and proceed.
- Shell names that allow same-pane restore: `zsh bash fish sh dash ksh tcsh nu` (also with a leading `-`).
- Tests never touch the real tmux server, `~/.claude`, or the real cache dir; inject paths and functions.
- Verification command for every task: `go test ./... && go vet ./...`.

## Review Focus

1. Snapshot from the **current** server must never trigger a restore, even when sessions in it are no longer live (user just quit them) — pinned in Task 4 (`TestRunSameServerDoesNothing`).
2. The post-restart launch must not overwrite the old snapshot before the restore result arrives — pinned in Task 5 (`TestNoSnapshotWriteBeforeRestoreDone`).
3. A pane that is not a shell (nvim, a running claude) or whose cwd differs must never receive keystrokes — pinned in Task 3 (`TestPlanNonShellPaneUsesNewWindow`, `TestPlanCwdMismatchUsesNewWindow`).
4. A session quit on purpose (pid file gone, or pid reused by another session) must not come back — pinned in Tasks 2 and 3.
5. Two servers (`tmux -L x`) must not see each other's snapshot — pinned in Task 1 (`TestLiveStateKeyedBySocket`).

---

### Task 1: Snapshot store, target parsing, tmux server info, config flag

**Files:**
- Create: `internal/session/livestate.go`, `internal/session/livestate_test.go`
- Modify: `internal/tmux/client.go` (add `ServerInfo`), `internal/tmux/parser.go` (add `ParseServerInfo`), `internal/tmux/parser_test.go`
- Modify: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type session.LiveEntry struct { ID, Harness, CWD, Project, SessionFile, TmuxSession string; WindowIndex, PaneIndex, PID int }` with json tags `id, harness, cwd, project, session_file, tmux_session, window_index, pane_index, pid`.
  - `type session.LiveSnapshot struct { ServerStart int64 \`json:"server_start"\`; SavedAt time.Time \`json:"saved_at"\`; Sessions []LiveEntry \`json:"sessions"\` }`
  - `session.NewLiveStateStore(path string) *LiveStateStore` (empty path → `DefaultLiveStatePath()`), `DefaultLiveStatePath() string`, `(*LiveStateStore).Get(socket string) (LiveSnapshot, bool)`, `(*LiveStateStore).SaveIfChanged(socket string, snap LiveSnapshot) (bool, error)`, `(*LiveStateStore).LoadErr() error`. On-disk shape: `map[socket]LiveSnapshot`.
  - `session.ParseTarget(target string) (tmuxSession string, window, pane int, ok bool)` for `"sess:win.pane"`.
  - `type tmux.ServerInfo struct { StartTime int64; SocketPath string }`, `(*tmux.Client).ServerInfo() (ServerInfo, error)`, `tmux.ParseServerInfo(raw string) (ServerInfo, error)`.
  - `config.Config.RestoreSessions bool \`yaml:"restore_sessions"\``.

- [ ] **Step 1: Write failing tests**

`internal/session/livestate_test.go`:
- `TestLiveStateRoundtrip`: save `LiveSnapshot{ServerStart: 100, Sessions: []LiveEntry{{ID: "a", TmuxSession: "s", WindowIndex: 2, PaneIndex: 1, PID: 42}}}` under socket `"/tmp/tmux-501/default"`; a fresh `NewLiveStateStore(samePath).Get("/tmp/tmux-501/default")` returns it with `ok == true`.
- `TestLiveStateKeyedBySocket`: save under socket A; `Get(B)` returns `ok == false`; saving under B leaves A intact after reload.
- `TestLiveStateMissingFile`: `Get` on a store over a nonexistent path → `ok == false`, `LoadErr() == nil`.
- `TestLiveStateCorruptFile`: file contains `{not json` → `LoadErr() != nil`.
- `TestLiveStateSaveIfChangedSkipsSameSessions`: first `SaveIfChanged` returns `true`; second call with identical `ServerStart`+`Sessions` but different `SavedAt` returns `false` and the file's mtime/content is unchanged.
- `TestParseTarget`: `"my sess:3.1"` → `("my sess", 3, 1, true)`; `"a:b:2.0"` → `("a:b", 2, 0, true)` (split on the last `:`); `""`, `"nocolon"`, `"s:x.y"` → `ok == false`.

`internal/tmux/parser_test.go`:
- `TestParseServerInfo`: `"1792195359\t/private/tmp/tmux-501/default\n"` → `{1792195359, "/private/tmp/tmux-501/default"}`; `"abc\t/x"` and `""` → error.

`internal/config/config_test.go`:
- Extend `TestDefaultConfigViewStateFlags` (or add `TestDefaultRestoreSessions`) asserting `DefaultConfig().RestoreSessions == true`; add to `TestLoadViewStateFlagsDefaultWhenAbsent` an assertion it stays `true`; add `restore_sessions: false` parsing to the YAML test at line 80.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/ ./internal/tmux/ ./internal/config/`
Expected: compile errors (`undefined: NewLiveStateStore`, `ParseServerInfo`, `RestoreSessions`).

- [ ] **Step 3: Implement**

- `livestate.go`: mirror `viewstate.go` (RWMutex, load on construction, atomic temp-file + rename, `MkdirAll(dir, 0o755)`, temp pattern `.live-sessions-*.json.tmp`). `SaveIfChanged` compares `ServerStart` and `Sessions` (use `slices.Equal`; `LiveEntry` is comparable) against the in-memory copy for that socket and only writes when different. `DefaultLiveStatePath` uses `os.UserCacheDir()`; on error return `""` (save becomes a no-op as in viewstate).
- `ParseTarget` lives in `livestate.go`.
- `ServerInfo()` runs `tmux display-message -p "#{start_time}\t#{socket_path}"` and parses with `ParseServerInfo`.
- Config: field next to `RememberGroupState`, comment `// resume sessions killed by a tmux restart on the next launch (default true)`, `RestoreSessions: true` in `DefaultConfig()`.

- [ ] **Step 4: Verify**

Run: `go test ./... && go vet ./...`
Expected: all `ok`.

---

### Task 2: Clean-exit check for Claude sessions

**Files:**
- Modify: `internal/harness/harness.go` (interface + cast), `internal/harness/claude/metadata.go`, `internal/harness/claude/capabilities.go` (interface assertion list)
- Test: `internal/harness/claude/metadata_exit_test.go` (new)

**Interfaces:**
- Produces:
  - `type harness.ExitChecker interface { ExitedCleanly(id string, pid int) bool }` and `harness.AsExitChecker(h Harness) (ExitChecker, bool)` (same one-line style as `AsResumer`).
  - `(*claude.Harness).ExitedCleanly(id string, pid int) bool`, delegating to unexported `exitedCleanly(dir, id string, pid int) bool` where `dir` is `~/.claude/sessions`.

- [ ] **Step 1: Write failing tests** (`t.TempDir()` as `dir`, never the real `~/.claude`)

- `TestExitedCleanlyMissingFile`: no `<pid>.json` → `true`.
- `TestExitedCleanlyPidReused`: `123.json` with `{"sessionId":"other"}`, id `"mine"` → `true`.
- `TestExitedCleanlyStaleFileMatches`: `123.json` with `{"sessionId":"mine"}` → `false`.
- `TestExitedCleanlyNoPid`: pid `0` → `false` (no evidence, restore it).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/harness/claude/ -run ExitedCleanly`
Expected: `undefined: exitedCleanly`.

- [ ] **Step 3: Implement**

`exitedCleanly`: `pid <= 0` → `false`; read `filepath.Join(dir, strconv.Itoa(pid)+".json")` → missing/unreadable → `true`; `parseSessionMetadata(data).SessionID != id` → `true`; else `false`. Add `_ harness.ExitChecker = (*Harness)(nil)` to the assertion block in `capabilities.go`.

- [ ] **Step 4: Verify**

Run: `go test ./... && go vet ./...`
Expected: all `ok`.

---

### Task 3: Snapshot entries and the restore plan (pure)

**Files:**
- Create: `internal/app/restore.go`, `internal/app/restore_test.go`
- Modify: `internal/session/manager.go` (add `ResumeInPane`), `internal/session/manager_test.go` only if a pure piece is extracted

**Interfaces:**
- Consumes: Task 1 `LiveEntry`, `ParseTarget`; Task 2 `AsExitChecker`; existing `resumeCommandFor(h, s, false)`, `harness.Registry.Get(name)`, `tmux.PaneInfo`, `session.Session.Key()`.
- Produces:
  - `liveEntries(active []*session.Session, reg *harness.Registry, exists func(path string) bool) []session.LiveEntry` — keeps sessions with non-empty `ID`, `exists(SessionFile)`, a parseable `TmuxTarget`, and a registered `Resumer` harness; `Harness == ""` maps to `"claude"`.
  - `type restoreKind int` with `restoreSamePane`, `restoreNewWindow`, `restoreRecreate`.
  - `type restoreAction struct { Entry session.LiveEntry; Kind restoreKind; Pane string; Command string }` (`Pane` is the `session:win.pane` target for `restoreSamePane`).
  - `planRestore(entries []session.LiveEntry, panes []tmux.PaneInfo, live []*session.Session, reg *harness.Registry, exists func(path string) bool) (actions []restoreAction, skipped int)`.
  - `isShell(cmd string) bool` over the Global Constraints list.
  - `(*session.Manager).ResumeInPane(sess *Session, paneTarget, command string) error` — `SendKeys(paneTarget, resumeShellCommand(sess.CWD, command))` then `SendKeys(paneTarget, "Enter")`, same as the tail of `ResumeInSession`.

- [ ] **Step 1: Write failing tests** (`restore_test.go`; registry built with `claude.New(claude.Options{})`; harness-less registry case via `harness/fake_test.go` patterns or a local fake that is not a `Resumer`. Entries planned against the real claude harness use `PID: 0` so `ExitedCleanly` never reads the real `~/.claude/sessions`.)

- `TestLiveEntriesFilters`: from four active sessions keep only the one with ID + existing file + target `"work:2.1"`; assert `WindowIndex == 2`, `PaneIndex == 1`, `TmuxSession == "work"`, `PID` copied.
- `TestPlanSamePane`: entry `{work, 2, 1, CWD "/p"}`, pane `{Target "work:2.1", CurrentCommand "zsh", CurrentPath "/p"}`, no live sessions → one action `restoreSamePane`, `Pane == "work:2.1"`, `Command == "claude --resume \"<id>\""`.
- `TestPlanLoginShellDashPrefix`: `CurrentCommand "-zsh"` → `restoreSamePane`.
- `TestPlanNonShellPaneUsesNewWindow`: `CurrentCommand "nvim"` → `restoreNewWindow`.
- `TestPlanCwdMismatchUsesNewWindow`: `CurrentPath "/other"` → `restoreNewWindow`.
- `TestPlanSessionMissingRecreates`: no pane in tmux session `work` at all → `restoreRecreate`.
- `TestPlanSkipsLive`: a live session with the same `Key()` → no action, `skipped == 1`.
- `TestPlanSkipsCleanExit`: entry whose harness `ExitedCleanly` returns true (use a fake harness implementing `Resumer` + `ExitChecker`) → skipped.
- `TestPlanSkipsMissingTranscript`: `exists` returns false for its `SessionFile` → skipped.
- `TestPlanSkipsNonResumer`: unknown harness name → skipped.
- `TestPlanPaneClaimedOnce`: two entries for `work:2.1` → first `restoreSamePane`, second `restoreNewWindow`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run 'Plan|LiveEntries'`
Expected: `undefined: planRestore`.

- [ ] **Step 3: Implement** in `restore.go`. Pane lookup: index panes by `Target`; tmux session existence = any pane with that `SessionName`. Compare paths with `filepath.Clean`. Build the `*session.Session` for `resumeCommandFor` from `ID, CWD, Harness, ProjectName`.

- [ ] **Step 4: Verify**

Run: `go test ./... && go vet ./...`
Expected: all `ok`.

---

### Task 4: The restorer (startup check, resurrect wait, execute)

**Files:**
- Modify: `internal/app/restore.go`, `internal/app/restore_test.go`

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces:
  - `type restorer struct { store *session.LiveStateStore; reg *harness.Registry; serverInfo func() (tmux.ServerInfo, error); listPanes func() ([]tmux.PaneInfo, error); detect func() ([]*session.Session, error); resumeInPane func(*session.Session, string, string) error; resumeInSession func(*session.Session, string, string) (string, error); exists func(string) bool; now func() time.Time; sleep func(time.Duration) }`
  - `(restorer).run() restoreDoneMsg`
  - `type restoreDoneMsg struct { info tmux.ServerInfo; restored, skipped int; errs []error; err error }` — `err` set only when `serverInfo` failed (gate stays closed).
  - `waitForResurrect(r restorer, info tmux.ServerInfo) []tmux.PaneInfo` — sleeps until server age ≥ 15s, then lists panes every 2s until two consecutive listings have the same sorted `Target`+`CurrentCommand` set, capped at 60s total; returns the last listing.
  - `(Model).restoreSessionsCmd() tea.Cmd` wiring the real deps (`m.tmuxClient.ServerInfo`, `ListPanes`, `m.detector.Detect`, `m.manager.ResumeInPane`, `m.manager.ResumeInSession`, `os.Stat`-based exists, `time.Now`, `time.Sleep`).

`run`: `serverInfo` error → `restoreDoneMsg{err}`. Snapshot missing / empty / `ServerStart == info.StartTime` → `restoreDoneMsg{info}` immediately (no wait). Otherwise `waitForResurrect`, `detect`, `planRestore`, then execute each action in order (`restoreSamePane` → `resumeInPane(sess, Pane, Command)`; others → `resumeInSession(sess, Entry.TmuxSession, Command)`), collecting per-action errors into `errs` and counting successes in `restored`.

- [ ] **Step 1: Write failing tests** (all deps faked; `sleep` records durations instead of sleeping; `now` advances on each `sleep`)

- `TestRunSameServerDoesNothing`: stored `ServerStart == info.StartTime` with entries whose sessions are not live → `restored == 0`, no resume fake called, `sleep` never called.
- `TestRunNoSnapshot`: empty store → zero counts, nothing called.
- `TestRunServerInfoError` → `msg.err != nil`.
- `TestRunRestoresAfterRestart`: stored start 100, current 200, server age 20s, panes stable → one `resumeInPane` for a shell pane and one `resumeInSession` for a missing session; `restored == 2`.
- `TestRunCollectsErrors`: `resumeInSession` returns an error → `restored == 0`, `len(errs) == 1`.
- `TestWaitForResurrectWaitsForAge`: server age 5s → first sleep ≈ 10s before listing.
- `TestWaitForResurrectWaitsForStablePanes`: listings `[a]`, `[a,b]`, `[a,b]` → returns `[a,b]` after two 2s sleeps.
- `TestWaitForResurrectCapsAt60s`: listings never stabilise → returns after total slept ≤ 60s.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run 'Run|WaitForResurrect'`
Expected: `undefined: restorer`.

- [ ] **Step 3: Implement** per the interface block above.

- [ ] **Step 4: Verify**

Run: `go test ./... && go vet ./...`
Expected: all `ok`.

---

### Task 5: App wiring (gate, snapshot writes, status line)

**Files:**
- Modify: `internal/app/app.go`
- Test: `internal/app/restore_test.go`

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces: `Model` fields `liveState *session.LiveStateStore`, `liveStateChecked bool`, `serverInfo tmux.ServerInfo`; `(*Model).saveLiveStateCmd(active []*session.Session) tea.Cmd`.

Wiring:
- `New()`: `liveState: session.NewLiveStateStore("")` when `cfg.RestoreSessions`.
- `Init()`: append `m.restoreSessionsCmd()` when `cfg.RestoreSessions`.
- `Update` `case restoreDoneMsg`: if `msg.err != nil` → leave the gate closed and return. Otherwise `m.serverInfo = msg.info`, `m.liveStateChecked = true`; `restored > 0` → `m.statusbar.SetNotice(fmt.Sprintf("restored %d sessions", n))` (`"restored 1 session"` for 1); any `errs` → `m.fail(fmt.Errorf("restored %d sessions, %d failed: %w", restored, len(errs), errs[0]))`. Return `m.saveLiveStateCmd(m.activeSessions)`.
- `saveLiveStateCmd`: returns `nil` unless `liveState != nil && liveStateChecked`. Otherwise a `tea.Cmd` that computes `liveEntries(active, reg, exists)` and calls `SaveIfChanged(serverInfo.SocketPath, LiveSnapshot{ServerStart, SavedAt: time.Now(), Sessions})`, returning `nil` (errors ignored, as `persistViewState` does).
- Call it from `case activeSessionsMsg` (success branch) and from `applySessionRefresh` (batched with its existing returned cmd).

- [ ] **Step 1: Write failing tests** (model built like `newTestModel` in `viewstate_test.go`, with `liveState` over a temp path and a registry from `claude.New`)

- `TestNoSnapshotWriteBeforeRestoreDone`: `liveStateChecked == false` → `saveLiveStateCmd(active)` is `nil`; the store file does not exist.
- `TestSnapshotWrittenAfterRestoreDone`: feed `restoreDoneMsg{info: {StartTime: 200, SocketPath: "/s"}}` into `Update`, run the returned cmd → store `Get("/s")` has `ServerStart == 200` and the live entry (session file created in temp dir).
- `TestRestoreDoneErrorKeepsGateClosed`: `restoreDoneMsg{err: ...}` → `liveStateChecked == false`.
- `TestRestoreDoneNotice`: `restored: 2` → status bar notice contains `"restored 2 sessions"` (read via the status bar's view/accessor used by existing statusbar tests).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run 'Snapshot|RestoreDone'`
Expected: compile failure on new fields.

- [ ] **Step 3: Implement** the wiring above.

- [ ] **Step 4: Verify**

Run: `go test ./... && go vet ./... && go build -o naviclaude ./cmd/naviclaude`
Expected: all `ok`, binary builds.

---

### Task 6: Real-world check and README

**Files:**
- Modify: `README.md` (Features bullet, new "Restore after restart" section, config example block near line 186, config table near line 210)

- [ ] **Step 1: Verify Claude's stale-file behaviour** (throwaway session only; never touch the user's running sessions or their tmux server)

```bash
tmux -L nc-restore-test new-session -d -s t -c "$(mktemp -d)" 'claude'
sleep 8; P=$(pgrep -n -f '^claude$|/claude$'); ls ~/.claude/sessions/$P.json
kill -9 $P; sleep 1; ls ~/.claude/sessions/$P.json   # expect: still there
tmux -L nc-restore-test send-keys -t t 'claude' Enter; sleep 8
ls ~/.claude/sessions/$P.json                        # still there => no pruning
tmux -L nc-restore-test kill-server
```

Record the outcome (stale file kept, or pruned on next start). Remove the stale `$P.json` afterwards only if it is the throwaway one. If `pgrep` picks the wrong process, stop and report rather than killing.

- [ ] **Step 2: Write README docs**: what is recorded and when (only while naviClaude is open), the three accepted limitations (sessions started while closed, changed conversation in a pane, the Step 1 finding about starting `claude` before naviClaude), same-pane restore needs tmux-resurrect, and `restore_sessions: true` in the example config and table (`| \`restore_sessions\` | \`true\` | ... |`).

- [ ] **Step 3: Manual end-to-end on a throwaway server** (`tmux -L nc-e2e`, never the user's default server): start a session in a temp dir with `claude`, send one prompt, open the local `./naviclaude` in that server, quit; `kill-server`; start the server again with the same session/window layout and open `./naviclaude` → after ~15s the status line shows `restored 1 session` and `claude --resume` runs in the pane. Report what was observed.

- [ ] **Step 4: Verify**

Run: `go test ./... && go vet ./...`
Expected: all `ok`.
