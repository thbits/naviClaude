# naviClaude Session Status Detection — Native-Status Revision

Date: 2026-06-25
Status: Approved; implementing on branch `status-detection-fix`
Supersedes the detection model in
[`2026-05-31-status-detection-design.md`](2026-05-31-status-detection-design.md)
(the content-anchored / subtree-CPU approach), which becomes the **fallback**.

## Problem

The WORKING / IDLE / WAITING status of a Claude session is still frequently
wrong. The prior fix (this branch) tried to infer status by screen-scraping the
tmux pane for selection-menu cursors and the "to interrupt" footer, summing
process-subtree CPU, watching `.jsonl` modtime, and reconciling all three with
hysteresis. That is the hard version of the problem and remains unreliable:
spinner/footer wording varies, prompt boxes render differently across Claude
versions and terminal widths, and CPU/modtime are noisy proxies.

### Key finding

Current Claude Code (verified on **v2.1.190**) computes the exact three states we
want and writes them to a per-process file naviClaude **already opens**:

```
~/.claude/sessions/<pid>.json
  "status":          "busy" | "waiting" | "idle" | "shell"
  "waitingFor":      "permission prompt" | "input needed" | "dialog open" | ...   (only when waiting)
  "statusUpdatedAt": <epoch ms>
  "sessionId":       "<uuid>"
```

- **busy**    — Claude is thinking, streaming, or running a tool (WORKING).
- **waiting**  — blocked on an interactive prompt: permission, question, dialog (WAITING).
- **idle**    — turn finished, sitting at the prompt (IDLE).
- **shell**   — idle while a foreground shell command is open (treated as IDLE).

This is the CLI's own internal state machine, debounced by Claude itself — not an
inference — which is why reading it is far more reliable than screen-scraping.
The same data is exposed via the documented `claude agents --json` command.

`readSessionMetadata` (`internal/session/detector.go`) already reads this exact
file for `sessionId`/`name`; it simply ignores the `status` field.

How peers solve it: **cmux** and **superset.sh** instead install Claude Code
*hooks* (`PermissionRequest`/`Notification`/`Stop`/`UserPromptSubmit`) that push
events to a status file or socket. That is the right approach for *older* Claude
versions or richer push eventing, but it requires mutating the user's Claude
config. For current Claude the native file gives the same answer with zero setup,
so it is our primary signal and hooks are explicitly out of scope here.

## Goals

- Status reflects Claude's own state machine when available: WORKING (`busy`),
  WAITING (`waiting`), IDLE (`idle`/`shell`).
- One source of truth, consumed by **both** status paths (the 1s detector loop
  and the 200ms preview path), eliminating the second-source drift the original
  spec flagged but did not resolve.
- Keep the existing screen-scrape + subtree-CPU + transcript-modtime + hysteresis
  machinery intact as a **fallback** for Claude versions that predate the status
  field, or panes where the file is missing.
- No regression to liveness detection (which session is actually running).

## Non-goals

- No Claude Code hooks (cmux/superset approach) — out of scope.
- No new UI status vocabulary: native states map onto the existing three
  (`StatusActive` / `StatusIdle` / `StatusWaiting`). `waitingFor` is parsed but
  not surfaced in the UI in this revision.
- No `shell` as a distinct UI state — folded into IDLE.
- No change to session discovery (the `ps` process-tree walk stays).

## Detection model (native-first)

Per session, per detect cycle:

1. **Native status** (highest priority). Read `status` from
   `~/.claude/sessions/<pid>.json`. Map:
   - `busy` → `StatusActive`
   - `waiting` → `StatusWaiting`
   - `idle`, `shell` → `StatusIdle`
   If `status` is one of these, it is **authoritative** and returned directly —
   no signal blending, no hysteresis (Claude already debounced it).

2. **Fallback** (only when `status` is empty/unrecognized — e.g. an older Claude
   version, or the file is absent): the unchanged prior model —
   `ClassifyPaneContent` (content) + `SubtreeCPU` (process) + `.jsonl` modtime,
   reconciled by the `StatusTracker` (priority WAITING > WORKING > IDLE, plus
   WORKING hysteresis).

### PID/sessionId robustness (root cause of the "idle shows ACTIVE" bug)

The Claude CLI layers processes: a tmux pane may show `zsh → claude → 2.1.190`
(the versioned binary), and `tmux`'s `pane_current_command` does not always agree
with `ps` comm. naviClaude picks the Claude PID by walking the process tree for a
`comm == "claude"` descendant — but the process that *writes* `~/.claude/sessions/<pid>.json`
may be a different node in that tree. When the picked PID has no `<pid>.json`,
`nativeStatus` was empty and detection fell back to the legacy CPU/transcript
path, which reports ACTIVE for a session that just went idle.

Fix: resolve native status with a **PID fast-path plus a sessionId fallback**.
`nativeStatusString(pid, sessionID)` reads `<pid>.json` first; if it has no
status, it scans `~/.claude/sessions/*.json` for the file whose `sessionId`
matches the session naviClaude already resolved (`readSessionStatusByID`). The
sessionId is reliable even when the PID is not, because it equals the `.jsonl`
transcript stem that `extractSessionIDFallback` recovers. `NativeStatus(pid,
sessionID)` (used by the preview path) applies the same two-step lookup.

### Liveness (which session is running) — already correct

Sessions are created only for PIDs found live in the `ps` tree (`Detect` →
`sessionFromPane`). A process that crashed mid-turn leaves a stale `busy` file,
but its PID is gone from `ps`, so no session is created from it — stale native
status cannot leak into the UI. PID reuse by a non-Claude process is harmless:
such a PID is never matched as `claude` in the tree, so its `<pid>.json` is never
read. (An optional `startedAt`-vs-process-start guard could harden this further;
deemed YAGNI for now.)

## Architecture

Files touched: `internal/session/detector.go`, `internal/session/session.go`,
`internal/app/app.go`. The fallback files (`classify.go`, `proctree_cpu.go`,
`status_tracker.go`, `preview/status.go`) are **unchanged**.

### Single source of truth: the native status reader

Extend the existing `sessionMetadata` struct and split parsing for testability:

```go
type sessionMetadata struct {
    SessionID       string `json:"sessionId"`
    Name            string `json:"name"`
    Status          string `json:"status"`          // busy|waiting|idle|shell (Claude >= 2.1)
    StatusUpdatedAt int64  `json:"statusUpdatedAt"`  // epoch ms
}

func parseSessionMetadata(data []byte) sessionMetadata { ... }   // pure, unit-testable

// mapNativeStatus maps Claude's status string to a SessionStatus. ok=false means
// no usable native status (empty/unknown) -> caller falls back to signals.
func mapNativeStatus(status string) (SessionStatus, bool) {
    switch status {
    case "busy":          return StatusActive,  true
    case "waiting":       return StatusWaiting, true
    case "idle", "shell": return StatusIdle,    true
    default:              return StatusIdle,    false
    }
}

// NativeStatus reads + maps the native status for a PID. Exported for the
// preview path; used by the detector via the value already parsed in buildSession.
func NativeStatus(pid int) (SessionStatus, bool) {
    return mapNativeStatus(readSessionMetadata(pid).Status)
}
```

### Detector path (1s loop, all sessions)

`buildSession` already calls `readSessionMetadata(claudePID)`; it stashes the
parsed `Status` onto a package-private `Session.nativeStatus` field (zero extra
I/O). `resolveStatus` checks it first:

```go
func (d *Detector) resolveStatus(s *Session, tree *ProcessTree) SessionStatus {
    if status, ok := mapNativeStatus(s.nativeStatus); ok {
        return status   // authoritative; no fallback work
    }
    // ... existing signal + StatusTracker logic, unchanged ...
}
```

### Preview path (200ms, focused session only)

`capturePreviewCmd` (`app.go`) captures `sess.PID` and prefers native status,
falling back to the screen-scrape `StatusDetector` only when native is absent:

```go
status, ok := session.NativeStatus(pid)
if !ok {
    status = statusDetector.DetectFromContent(target, content)
}
```

The `previewCaptureMsg` handler still applies only `StatusWaiting` from this path
(Active/Idle remain owned by the 1s detector to avoid flicker) — unchanged. Net
effect: the focused pane gets fast, reliable WAITING detection from the native
signal instead of a screen-scrape guess, and the native status can no longer be
clobbered by content classification.

## Data flow

```
Detect() per pane
  └─ buildSession: readSessionMetadata(pid) -> {sessionId, name, status}
                                                         │ stash s.nativeStatus
  └─ resolveStatus(s):
        mapNativeStatus(s.nativeStatus) --ok--> return (busy|waiting|idle)
                          │ not ok
                          ▼
        ClassifyPaneContent + SubtreeCPU + modtime -> StatusTracker -> status

capturePreviewCmd() (focused pane, 200ms)
  └─ NativeStatus(pid) --ok--> status
                  │ not ok
                  ▼
        StatusDetector.DetectFromContent(content) -> status
  (handler applies only StatusWaiting)
```

## Error handling

- File missing / unreadable / unparseable → `readSessionMetadata` returns the
  zero struct → `mapNativeStatus("")` returns `ok=false` → fallback path. Never
  panics.
- Capture failure in the preview path is handled as today (shows a message);
  native status read is independent of capture and still works.

## Testing strategy

1. **`mapNativeStatus` unit test**: `busy`/`waiting`/`idle`/`shell` map to the
   right `SessionStatus` with `ok=true`; `""`/`"unknown"` return `ok=false`.
2. **`parseSessionMetadata` unit test** against fixture JSON bytes: a current
   v2.1.190-shaped record (status present), an older record (no status field →
   empty), and malformed JSON (→ zero struct, no error to caller).
3. **`resolveStatus` native-first test**: with `s.nativeStatus="busy"` it returns
   `StatusActive` even when CPU/content signals would say otherwise; with
   `s.nativeStatus=""` it falls through to the existing signal/tracker logic.
4. Existing `classify_test.go` / `status_tracker_test.go` / proctree tests stay
   green — they now cover the fallback path.
5. **Live verification** via the `prefix+G` dev build (user-in-the-loop): trigger
   a permission prompt and an AskUserQuestion (→ WAITING), a long-running tool
   (→ WORKING), sit at the prompt (→ IDLE); confirm across several simultaneous
   sessions and that the focused pane agrees with background panes.
6. `go test ./...`, `make build` clean.

## Rollout

Branch `status-detection-fix` (already off `main`). Implement the native reader +
detector branch + preview branch + tests together; build the binary in place for
the user to test before committing.
