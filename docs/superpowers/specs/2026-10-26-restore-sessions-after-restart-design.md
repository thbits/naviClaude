# Restore Sessions After a Restart

## Problem

After a reboot (or anything that kills the tmux server), every running agent
session dies. tmux-resurrect/continuum brings back the tmux sessions, windows,
panes and their working directories, but each pane that ran `claude` comes back
as a plain shell: resurrect only records the command name, never which
conversation was in the pane. naviClaude then lists those conversations under
"Closed", and the user has to find and resume each one by hand, and the resume
lands in a new window rather than the pane it lived in.

We want naviClaude to remember which conversation was running where, and on the
first launch after a restart, automatically resume each one back in place, so
the user continues from the same spot.

## Goals

- Automatic: no prompt, no key press. Opening naviClaude after a restart is
  enough.
- Put each conversation back as close to where it was as possible: the same
  pane, else the same tmux session, else a recreated tmux session of that name.
- Works without Claude Code hooks and without tmux-resurrect (resurrect only
  makes the "same pane" case possible).
- Never resume anything during normal use on the same tmux server.

## Non-goals

- Recording sessions while naviClaude is closed. No hooks, no daemon, no tmux
  hook (user decision). The snapshot is as fresh as the last time naviClaude
  was open. Accepted consequences:
  - A session started after naviClaude was last open is not restored.
  - A pane whose conversation changed after that (`/clear`, `/resume`) comes
    back with the previously recorded conversation.
  - A session quit on purpose while naviClaude was closed comes back after a
    restart (no reliable quit signal without hooks; see Plan).
- A `naviclaude restore` CLI subcommand / restoring before naviClaude is opened.
- Restoring pane layout, scrollback, or non-agent programs (resurrect's job).
- Harnesses that cannot resume by ID (no `harness.Resumer`).

## Anti-goals

- Duplicates: resuming a conversation that is already running, or resuming the
  same snapshot twice.
- Typing a resume command into a pane that is running something else (an
  editor, another agent, a REPL).
- Losing the snapshot before it is used (the post-restart launch must not
  overwrite it with the new, empty state before restoring).

## Configuration

One new field on `config.Config`:

| yaml key           | field             | default | effect                                   |
| ------------------ | ----------------- | ------- | ---------------------------------------- |
| `restore_sessions` | `RestoreSessions` | `true`  | record live sessions and restore them after a tmux restart |

Default is on (user decision: automatic). When `false`, naviClaude neither
writes nor reads the snapshot.

## Snapshot file

Kept in naviClaude's cache dir, next to the existing caches:
`filepath.Join(os.UserCacheDir(), "naviclaude", "live-sessions.json")`
(`~/Library/Caches/naviclaude/live-sessions.json` on macOS), mirroring how
`attention.go` / `stats_cache.go` resolve that dir.

One file holds one snapshot per tmux server, keyed by the server's
`#{socket_path}`, so a second server (`tmux -L other`) never restores another
server's sessions (builder's call, from review):

```json
{
  "/private/tmp/tmux-501/default": {
  "server_start": 1792195359,
  "saved_at": "2026-10-26T09:40:00Z",
  "sessions": [
    {
      "id": "aa996627-5b8c-43da-9f81-708d9aed7a03",
      "harness": "claude",
      "cwd": "/Users/tomhalo/personal/git/naviClaude",
      "project": "naviClaude",
      "tmux_session": "naviclaude",
      "window_index": 1,
      "pane_index": 1
    }
  ]
  }
}
```

- `server_start` is the tmux server's `#{start_time}` (epoch seconds) at the time
  of writing; it is what tells "same server" from "after a restart". A new tmux
  client method `ServerStartTime() (int64, error)` runs
  `tmux display-message -p '#{start_time}'`. The same call also returns
  `#{socket_path}`, which picks the snapshot this server reads and writes.
- Location is stored as session name + window index + pane index (from
  `PaneInfo` / `Session.TmuxTarget`). Pane/window ids (`%N`, `@N`) are not used:
  they change on every server start, while resurrect restores names and indices.
- New file `internal/session/livestate.go` with a `LiveStateStore` modelled on
  `ViewStateStore` (load on construction, `Get`, `Save` with atomic
  temp-file + rename, `LoadErr`). Pure and unit-testable; it takes a path.

### What is recorded

Every live session that has a non-empty `ID`, a `SessionFile` that exists on
disk (stat it: `claude.Enrich` sets the path even before the transcript is
written, and a brand-new session cannot be resumed), a
`TmuxTarget`, and a harness that implements `harness.Resumer`. Subprocess
sessions (agent inside nvim etc.) are recorded too; at restore their pane is not
a shell, so they land in a new window of the same tmux session.

### When it is written

On each `activeSessionsMsg` with no error, after the startup restore step has
run (see gate below), naviClaude writes the snapshot with the current
`server_start` and the recorded sessions, but only when the recorded set
(ids + locations) differs from what is on disk, so the steady state costs no
disk writes. The write runs off the UI goroutine (inside a `tea.Cmd` or the scan
goroutine), like other I/O.

An empty live list is written too (so deliberately closing everything clears
the snapshot), except while the gate below is closed.

## Restore on startup

### Gate

`Model` gains `liveStateChecked bool`. Until it is true, snapshot writes are
suppressed. On the first successful `activeSessionsMsg`:

1. Read the stored snapshot and the current `ServerStartTime()`.
2. If `RestoreSessions` is off, the file is missing/corrupt, the snapshot is
   empty, or `stored.server_start == current`: nothing to restore.
3. Otherwise, build the restore plan (below) and run it as a `tea.Cmd`.
4. Set `liveStateChecked = true` only when the restore result message arrives
   (builder's call, from review). The restore is a handful of tmux calls and
   takes well under a second; if naviClaude exits before it finishes (popup
   closed), the snapshot was never overwritten and the next launch re-plans,
   where the "already live" and "pane is still a shell" checks skip what was
   already resumed. After that, writes record the new server's state, so the
   old snapshot is consumed once.

If `ServerStartTime()` fails, skip restore and keep writes suppressed for this
run (don't risk overwriting a snapshot we could not evaluate).

### Waiting for tmux-resurrect (builder's call, from review)

continuum restores in the background for a few seconds after the server starts.
If naviClaude plans during that window it would recreate sessions resurrect is
about to restore. So the gate check only proceeds once the server has been up
at least 15 seconds and two consecutive `ListPanes()` results (taken on
successive active scans) list the same panes; until then it keeps
re-checking on each scan with writes still suppressed. The user's tmux.conf
does not set `@resurrect-processes`, so restored panes come back as shells.

### Plan

For each stored entry, in file order:

- Skip if its harness is not registered / not a `Resumer`.
- Skip if a live session with the same `Key()` exists (already running, e.g.
  restored by another naviClaude instance or started by hand).
- No quit-on-purpose check (user decision, after testing): Claude Code deletes
  its `~/.claude/sessions/<pid>.json` on `/exit` but also when tmux kills it
  (SIGHUP on `kill-server`, so a normal reboot too) and prunes leftover files
  whenever a new `claude` starts, so the file cannot tell a quit from a restart.
  The transcript has no `/exit` marker either. Sessions quit while naviClaude
  was closed therefore come back after a restart.
- Skip if the transcript is gone (the harness can't resume it); use the
  harness's normal lookup or the closed-session list, builder's choice.
- Otherwise pick a placement, using one `ListPanes()` snapshot taken at plan time:
  1. **Same pane**: a pane exists at `tmux_session:window_index.pane_index`, its
     `pane_current_command` is a known interactive shell (`zsh`, `bash`, `fish`,
     `sh`, `dash`, `ksh`, `tcsh`, `nu`, optionally `-`-prefixed login form), and
     its `pane_current_path` equals the entry's `cwd` (after `filepath.Clean`).
     Send `resumeShellCommand(cwd, ResumeCommand(s))` + `Enter` to that pane
     via `SendKeys`, same as `ResumeInSession` does for new sessions.
  2. **Same tmux session**: the tmux session exists → `Manager.ResumeInSession`
     (new window, named by the existing window-name template).
  3. **Recreate**: the tmux session is gone → `Manager.ResumeInSession` creates it
     under the stored name.
- A pane is claimed at most once per plan, so two entries can't target the same
  pane.

Each placement builds a `*session.Session` from the entry (ID, CWD, Harness,
ProjectName) and gets its command from the existing `resumeCommandFor` helper.

### Outcome

The command returns a message with counts (restored / skipped) and any errors.
The app reports it through the existing status/error line, e.g.
`restored 5 sessions` or `restored 4 sessions, 1 failed: <err>`. Nothing is shown
when there was nothing to restore. The next active scan picks the resumed
sessions up; no special sidebar handling.

Restore does not switch the client or change focus/selection; the user stays in
naviClaude where they opened it.

## Edge cases

- **Two naviClaude instances open at once after a restart:** both may plan before
  either has written. The "already live" check makes the second one's resume
  mostly a no-op, but a race can still start a duplicate. Accepted; rare.
  (Builder may add an advisory lock on the snapshot file if cheap.)
- **Session moved/renamed after the last naviClaude launch:** stored location is
  stale; placement falls through to "same session" or "recreate". Acceptable.
- **naviClaude opened in a brand-new tmux server without resurrect:** the stored
  tmux sessions don't exist, so they are recreated by name and the
  conversations resumed there.
- **Snapshot from a long time ago** (naviClaude not opened for weeks): still
  restored on the next new server. Accepted; same rule as any restart.

## Testing

- `internal/session/livestate_test.go`: save/get roundtrip; missing file → zero
  value, nil `LoadErr`; corrupt file → `LoadErr`.
- Pure plan function (e.g. `planRestore(entries, panes, live, registry)`) in
  `internal/app` or `internal/session`, unit-tested without tmux:
  - same server start → empty plan (tested at the gate level);
  - shell pane at location with matching cwd → same-pane placement;
  - pane running something else, or cwd mismatch → same-session placement;
  - tmux session missing → recreate placement;
  - already-live key → skipped; non-Resumer harness → skipped;
  - two entries pointing at one pane → second falls back to new window.
- Snapshot recording filter (ID/SessionFile/target/Resumer) and the
  "write only on change" comparison.
- App-level: with the gate closed, an `activeSessionsMsg` does not write the
  snapshot; after the check it does.
- Config: `restore_sessions` parses; defaults to `true` when absent.
- Tests must never touch the real tmux server or the user's cache dir (inject
  the store path and a fake tmux/pane list, as existing tests do).

## Docs

- README: a Features bullet and a short "Restore after restart" section
  explaining the snapshot is taken while naviClaude is open, the two accepted
  limitations, how resurrect enables same-pane restore, and `restore_sessions`.
- Config example (`config.yaml` template, if one is generated) gains
  `restore_sessions: true`.

## Files touched

- `internal/config/config.go` (+ test): `RestoreSessions`, default `true`.
- `internal/tmux/client.go`: `ServerStartTime()`.
- `internal/session/livestate.go` (new) + test.
- `internal/app/app.go` (+ new `restore.go` / `restore_test.go`): store field,
  gate, snapshot write on active scans, plan + run on startup, status message.
- `README.md`.
