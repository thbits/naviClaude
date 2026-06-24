# naviClaude Session Status Detection — Design

Date: 2026-05-31
Status: Approved direction; pending spec review
Scope: Independent of the TUI stabilization/refresh spec. Fix now, own branch.

## Problem

The WORKING / IDLE / WAITING status of a Claude session is frequently wrong:
- A session that is actively working often shows **IDLE**.
- Sessions blocked **waiting for the user's answer** (permission prompts,
  questions) are usually **not** detected as WAITING.

Root cause: the detector measures the wrong signals.

### ACTIVE vs IDLE is decided only by `.jsonl` modification time

`buildSession` (`internal/session/detector.go:368-380`) defaults to `StatusIdle`
and promotes to `StatusActive` only if the session's `.jsonl` transcript file was
written within `active_window_secs` (default **5s**). The transcript is appended
only when a message or tool-result is recorded, so:
- During a **long tool call** (build, test run, web fetch, big grep) the
  transcript is untouched for the whole duration → flips to IDLE while busy.
- During **long model thinking / streaming**, write-gaps exceed 5s → IDLE.
- It oscillates ACTIVE↔IDLE as writes come and go (also a flicker source).

The detector already reads per-process CPU (`ProcessTree.Stats`, used for the
CPU/MEM readout) but **never uses it for status**, and it only reads the single
Claude PID's CPU — not the subtree, where tool subprocesses actually burn CPU.

### WAITING uses a narrow, stale pattern set

`isWaitingForInput` (`detector.go:246`) scans only the **last 6 non-empty lines**
for `[y/n]`, `[n/y]`, `Allow?`, `enter to select`, `enter to confirm`,
`What should Claude do instead?`. Current Claude Code prompts render as a box:

```
Do you want to proceed?
❯ 1. Yes
  2. Yes, during this session
  3. No, and tell Claude what to do differently (esc)
```

None of the matched strings reliably appear. Three compounding flaws:
1. The code **deliberately ignores `❯`** because the idle input box also shows it
   — but `❯ 1.` / `❯ Yes` (a *selection cursor*) is the strongest WAITING signal.
2. The **6-line tail window is too small** — the box is question + options +
   footer + the input line, so the keyword sits >6 non-empty lines from the
   bottom and falls out of range.
3. Patterns omit "Do you want to…", numbered menus, `(esc)` footers, plan-mode
   "Would you like to proceed", and trust dialogs.

### Other issues
- Detection logic is **duplicated** in `detector.go` (`isWaitingForInput`) and
  `preview/status.go` (`matchesInputPrompt`); they can drift.
- `Detect()` does a **synchronous per-pane `capture-pane` every 1s**
  (`detector.go:225`) in addition to the 200ms preview capture.

## Goals

- WORKING reflects actual work (interrupt-footer or subtree CPU), not transcript
  writes.
- WAITING reliably detects current Claude prompts: permission, confirmation,
  selection menus, trust, plan-mode.
- No status flicker (priority + hysteresis).
- One source of truth for detection, used by both the detector and the preview
  path.
- Verified against real prompts via the `prefix+G` dev build (user-in-the-loop),
  informed by online research of current Claude Code prompt UI.

## Non-goals
- No change to how sessions are discovered (process-tree walk stays).
- No attempt to enumerate spinner verbs (they are randomized/user-customizable);
  the stable `to interrupt` footer is the anchor instead.

## Detection model (content-anchored, priority order)

Per detect cycle, from the captured pane content (ANSI-stripped), examine the
last ~20 non-empty lines (wide enough to contain the whole prompt box). Decide in
this priority order:

1. **WAITING** (highest priority) if any of:
   - `[y/n]`, `[Y/n]`, `[n/y]` confirmation.
   - "do you want to proceed" / "do you want to " question phrasing.
   - "no, and tell claude what to do" / "what should claude do instead".
   - "would you like to proceed" (plan mode) / "do you trust" (trust dialog).
   - a **selection-menu line**: the `❯` selector immediately followed by an
     option — regex `❯\s*(\d+\.|yes\b|no\b)` (case-insensitive). This is what
     distinguishes a waiting menu from the idle input `❯`.
   - "enter to select" / "enter to confirm" / "press enter to".
   - "allow?" / explicit permission action lines.

2. **WORKING / ACTIVE** if either:
   - any recent line contains `to interrupt` (covers "esc to interrupt" and
     "ctrl+c to interrupt" — the stable footer shown only while generating or
     running tools), OR
   - the Claude process **subtree** CPU% exceeds `cpu_active_threshold`
     (default ~5%) — captures long tool calls where a child process burns CPU
     while the claude PID idles.

3. **IDLE** otherwise (process running, input box idle, no interrupt footer).

`.jsonl` modtime is demoted to a **fallback / hysteresis input** only (used when
content capture is unavailable, e.g. subprocess panes or capture failure).

### Hysteresis (anti-flicker)
Maintain a tiny per-target state `{lastStatus, lastChange}`. WORKING and WAITING
are **sticky** for a short grace period (e.g. WORKING persists up to
`active_window_secs` after the last interrupt-footer/CPU sighting) before
dropping to IDLE. WAITING clears immediately once the prompt box is gone.

## Architecture

Files: `internal/session/detector.go`, `internal/preview/status.go` (consolidated
classifier), `internal/session/metrics.go`/`detector.go` (`ProcessTree`),
`internal/config/config.go`.

- **Single classifier.** Move all content pattern logic into one exported
  function (e.g. `session.ClassifyPaneContent(content) StatusHint`) used by both
  `Detector.Detect()` and the preview `StatusDetector`. Delete the duplicate.
- **Subtree CPU.** Add `ProcessTree.SubtreeCPU(pid)` summing CPU across the
  descendant set (the `children` map already exists). `buildSession` uses it for
  the WORKING decision and may also surface it in the CPU readout.
- **Status state / hysteresis.** Add a small `StatusTracker` (per tmux target)
  owned by the `Detector` that applies the priority + stickiness rules and
  returns the final `SessionStatus`.
- **Config.** Keep `active_window_secs` (now a fallback/hysteresis knob); add
  optional `cpu_active_threshold` (default 5.0) and `waiting_detection` toggle.

## Data flow

```
Detect() per pane ─▶ capture pane content (async-friendly)
        │                         │
        │                         ▼
        │            ClassifyPaneContent → {waiting? | interrupt-footer?}
        ▼                         │
  ProcessTree.SubtreeCPU(pid) ────┤
        │                         ▼
        └────────────▶ StatusTracker.Resolve(target, signals)
                                  │  priority: WAITING > WORKING > IDLE
                                  │  + hysteresis
                                  ▼
                            final SessionStatus
```

## Error handling
- Capture failure → fall back to subtree CPU + `.jsonl` modtime; never crash,
  keep last known status within the hysteresis window.
- Subprocess panes (Claude under nvim etc.) → pane belongs to the parent;
  fall back to CPU/modtime and do not assert WAITING from foreign content.

## Testing strategy

1. **Classifier unit tests against a real-capture corpus.** Add fixtures under
   `internal/session/testdata/` (or `preview/testdata/`): captured screens for
   working (interrupt footer), idle input box, permission prompt, `[y/n]`, trust
   dialog, plan-mode prompt, numbered selection menu. Assert the classifier
   returns the right state for each. The user contributes/validates real captures
   via the `prefix+G` dev build; research-derived samples seed the corpus.
2. **Subtree-CPU test** with a synthetic `ProcessTree`.
3. **Hysteresis test**: WORKING → brief capture gap → still WORKING; sustained
   gap → IDLE; WAITING clears immediately when the box disappears.
4. **Live verification**: user triggers permission/question prompts and a
   long-running tool call, confirms WORKING/WAITING/IDLE are correct in the dev
   build; reports misses to extend the corpus.
5. `go test ./...`, `make build` / `make lint` clean.

## Rollout
Own branch `status-detection-fix` off `main`. Single fan-out: one agent for the
classifier+tests, one for subtree-CPU + StatusTracker + config, integrated, then
live verification.

## Open follow-up (out of scope here)
Reducing the per-pane synchronous `capture-pane` every 1s — fold into the
capture-efficiency work in the stabilization spec rather than here.
