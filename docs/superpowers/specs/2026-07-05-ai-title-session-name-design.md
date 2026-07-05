# Live AI session title in the sidebar

## Problem

Once Claude Code assigns a session a real title mid-session, naviClaude never
reflects it. A session keeps showing a placeholder like `tomhalo-fe` even though
its actual title is, e.g., "Configure Hindsight memory with local Postgres".

### Root cause

naviClaude sources a session's display title only from
`~/.claude/sessions/<pid>.json` → `name`. For interactive sessions that field is
an auto-derived placeholder (`"nameSource": "derived"`, e.g. `<project>-<hash>`)
and is never rewritten with the real title. The real, evolving title lives inside
the session transcript `.jsonl` as the latest record of type `ai-title`:

```json
{"type":"ai-title","aiTitle":"Configure Hindsight memory with local Postgres","sessionId":"..."}
```

naviClaude never reads `ai-title` records, so `DisplayName` is frozen on the
placeholder.

Observed on-disk pattern (all live interactive sessions):

| per-PID `name` | `nameSource` | latest `aiTitle` |
|---|---|---|
| tomhalo-fe | derived | Configure Hindsight memory with local Postgres |
| naviclaude-9f | derived | Fix Catppuccin Mocha text color display |

Background (`kind: "bg"`) sessions already carry the real title in the per-PID
`name` field, so they are unaffected.

## Non-goals

- No separate daemon or background process. naviClaude is a TUI that already
  re-scans every session on a ~1s tick (`tickSessionMsg` -> `refreshSessionsCmd`
  -> `buildSession`). Reading the correct field makes the title live for free.
- No writing back to Claude's files. naviClaude only reads them.

## Design

### 1. New reader: `lastAITitle`

Add `lastAITitle(path string) string` to the `session` package, mirroring the
existing `lastMessageTime` (`internal/session/lastactivity.go`). It scans the
`.jsonl` line by line and returns the `aiTitle` of the **last** `ai-title`
record. Returns `""` when the file is missing, has no `ai-title` records, or
cannot be read. Malformed lines are skipped (same tolerance as
`lastMessageTime`), using the same `maxTranscriptLineBytes` scanner buffer.

### 2. Metadata field: `nameSource`

Add `NameSource string \`json:"nameSource"\`` to the `sessionMetadata` struct
(`internal/session/detector.go`). This lets the precedence logic distinguish a
derived placeholder from a real (user-set or bg) name.

### 3. Title precedence

When setting `DisplayName`, resolve in this order (highest wins):

1. naviClaude user alias (`~/.config/naviclaude/session-names.json`, via
   `applyAliases`) — explicit in-tool override, unchanged.
2. per-PID `name` when `nameSource != "derived"` and non-empty — a real
   user-set or background-session name.
3. `aiTitle` from the transcript (the fix).
4. per-PID `name` (the derived placeholder).
5. Existing fallback to Slug / ProjectName in the sidebar renderer (unchanged).

Aliases already sit above everything because `applyAliases` runs after the
detector/history assign `DisplayName`. Steps 2–4 are the new resolution inside
the detector/history layer; step 5 is the sidebar's existing behavior when
`DisplayName` is empty.

### 4. Call sites

**Active sessions — `buildSession` (`detector.go`).** After reading `meta`,
resolve the title via the precedence above. Read `aiTitle` through a new
mtime-gated cache (`aiTitleCache`, keyed by sessionID, invalidated on transcript
mtime change) that parallels `lastActCache`, so the transcript is only re-scanned
when it grows — not on every 1s tick.

**Closed sessions — history parse loop (`history.go`).** The loop already reads
each transcript. Capture the last `ai-title` in the same pass and assign it as
the session title (subject to the same precedence), so a closed session keeps its
real title instead of reverting to a placeholder/Slug.

## Testing

- `lastAITitle`: last record wins over earlier ones; no `ai-title` records ->
  `""`; missing file -> `""`; malformed/oversized lines skipped.
- Active path: derived `name` + present `aiTitle` -> `DisplayName == aiTitle`;
  non-derived `name` -> `name` wins over `aiTitle`; no `aiTitle` -> derived
  `name`.
- Closed path: transcript with `ai-title` -> title reflected on the closed
  session.
- Alias still overrides an `aiTitle`.

## Rollout

Pure read-path change, no config or migration. Behavior is correct as soon as
the rebuilt binary runs against existing on-disk transcripts.
