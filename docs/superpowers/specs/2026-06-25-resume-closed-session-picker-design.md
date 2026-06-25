# Resume a Closed session via a target picker

## Goal

When a Closed session is selected in the sidebar, pressing `Enter` opens a
picker that lets the user choose **where** the session is resumed:

- fuzzy-search and select an existing tmux session, or
- type a name to create a brand-new tmux session,

then resume the session there with `claude --resume <sessionID>` (plain resume).

## Background / current state

- Closed sessions are parsed from `~/.claude/projects/**/*.jsonl`. Each carries
  `ID` (the session UUID = filename stem) and `CWD`, which is all `claude
  --resume <id>` needs. They are grouped under a single "Closed" group and do
  **not** retain their original tmux session name (nothing sets
  `Session.TmuxSession` for closed sessions).
- `Manager.Resume(sess, target)` already runs
  `cd <cwd> && claude --resume "<id>"` in a **new window** of an **existing**
  tmux session. `ForkResume` adds `--fork-session`.
- There is no "resume into a session, creating it if missing" path.
  `CreateNewTmuxSession` has the exists-or-create logic but only for the plain
  `claude` command (via send-keys), not resume.
- Today `Enter` on a Closed session only loads its conversation history into the
  preview pane (`app.go:741-745`). That preview already auto-loads on
  navigation (`app.go:816-818`), so repointing `Enter` loses nothing.
- `f` (Jump) on a Closed session already resumes into the current tmux session
  (no picker). The right-click context menu offers Resume / Fork & Resume.
- `m.currentTmuxSession` is already tracked.
- The tmux client has `HasSession`, `NewSessionPrint`, `NewWindow`,
  `ResizeToClient`, `SendKeys`, but **no** "list all tmux sessions" method.

## Decisions (from brainstorming)

1. `Enter` on a Closed session opens a resume-target picker. `Tab` keeps the
   existing preview behavior. Navigation still auto-loads the preview.
2. The picker lists **all live tmux sessions**, fuzzy-searchable.
3. Pre-highlight `m.currentTmuxSession`; sort the rest by recency.
4. Typing a name that doesn't match an existing session creates a new tmux
   session with that name. Disambiguation is explicit via a synthetic
   `＋ new session: <text>` row rather than guessing.
5. Plain resume only. Fork & Resume stays in the right-click context menu,
   unchanged.
6. `f` stays a no-picker fast-resume into the current session (two intentional
   speeds: `f` = resume here now, `Enter` = pick where).

## Architecture

Chosen approach: a dedicated, focused `SessionPicker` UI component modeled on
the existing `dirpicker` pattern, plus a manager resume method and a tmux
list-sessions helper. (Rejected: generalizing `dirpicker` — it is tangled with
directory concerns, marginal gain, regression risk. Rejected: no picker — does
not deliver the feature.)

### Components

**a. `tmux.Client.ListSessions()` + `tmux.ParseSessions()` (`internal/tmux/`)**

- `ListSessions()` runs `tmux list-sessions -F '#{session_name} #{session_activity}'`.
- `ParseSessions(string) []SessionInfo` is pure (unit-tested), returning
  `SessionInfo{Name string; Activity int64}` (`session_activity` = epoch
  seconds of last activity). Unparseable lines are skipped. No sessions / error
  yields an empty slice so the picker still opens.

**b. `Manager.ResumeInSession(sess *Session, targetName string) error`
(`internal/session/manager.go`)**

- Guard: empty `sess.ID` → error (matches existing `resumeWithFlags`).
- If `HasSession(targetName)` → delegate to existing `Resume(sess, targetName)`
  (new window in the existing session).
- Else → create a detached tmux session named `targetName` rooted at
  `sess.CWD` (`NewSessionPrint`), `ResizeToClient(targetName)`, then send-keys
  the resume command + `Enter` (mirrors `CreateNewTmuxSession` so the detector
  recognizes Claude as a child of the pane's shell).
- Extract the resume command string into one helper
  (`resumeShellCommand(sess) string` returning `cd <cwd> && claude --resume
  "<id>"`) shared by the `NewWindow` path and the send-keys path (DRY). The
  helper centralizes the existing inline `fmt.Sprintf` in `resumeWithFlags`.

**c. `ui.SessionPickerModel` (`internal/ui/sessionpicker.go`)**

- Fields: `input textinput.Model`, `sessions []string` (candidate names,
  recency-ordered), `filtered []string`, `cursor int`, `visible bool`,
  `width/height int`, `title string`.
- `Show(sessions []string, current string) ` — set candidates, pre-highlight
  `current` (place its cursor), focus input, clear filter.
- Fuzzy filter via `sahilm/fuzzy` over names (same lib as dirpicker/search).
- Synthetic create-new row: when `strings.TrimSpace(input)` is non-empty and no
  candidate equals it exactly, append a `＋ new session: <text>` row at the
  bottom of the filtered list.
- `Selected() (name string, isNew bool)` — returns the highlighted session, or
  the typed name with `isNew=true` when the create-new row is highlighted.
- Pure logic (`filterSessions`, create-new decision, `Selected`) is
  unit-tested. `View()` follows the dirpicker overlay style.

**d. App wiring (`internal/app/`)**

- `modes.go`: add `ModeResumePicker` (+ its `String()` case).
- `app.go`: add `resumePicker ui.SessionPickerModel` field and
  `pendingResumeSession *session.Session`; construct the picker in `New`.
- `app.go:741-745` (the `KeyEnter, KeyTab` case for a Closed session): on
  `KeyEnter` open the picker (`openResumePicker(sess)` — loads `ListSessions()`
  synchronously, which is instant); on `KeyTab` keep the existing preview load.
- `openResumePicker`: store `pendingResumeSession`, populate the picker
  (current session pre-highlighted), set `ModeResumePicker`.
- `handleResumePickerKey`: `Esc` cancels (clears pending, returns to list);
  `↑/↓` move; typing filters; `Enter` reads `Selected()` and resumes via a Cmd
  calling `Manager.ResumeInSession(pendingResumeSession, name)`, then refreshes.
- Render the picker overlay in the view switch (alongside dirpicker/contextMenu)
  and route `SetSize`.
- `f` (Jump) on a Closed session unchanged. Context-menu Resume/Fork & Resume
  unchanged.

### Data flow

```
Enter on Closed session
  -> ListSessions(); open SessionPicker (current pre-highlighted, rest by recency)
  -> user filters / arrows / types
  -> Enter:
       existing session   -> ResumeInSession -> new window: claude --resume <id>
       "+ new session: X"  -> ResumeInSession -> create tmux session X + resume
  -> refresh; resumed session appears in sidebar on next tick (no auto-switch,
     matching today's resume behavior)
```

### Error handling

- Empty `sess.ID` → error surfaced in statusbar (existing guard).
- tmux failures surface via the existing `errMsg` → statusbar path used by
  `resumeCmd`.
- `ListSessions` empty/error → picker opens empty; the user can still type a new
  name to create a session.

### Testing (TDD — tests first)

- `tmux.ParseSessions`: well-formed lines, blank/garbage lines, empty input.
- `resumeShellCommand`: correct `cd`/quoting for normal and quote-containing
  CWDs; correct `--resume "<id>"`.
- `SessionPickerModel`: `filterSessions` ordering, the create-new row appears
  only when input is non-empty and not an exact match, `Selected()` returns the
  right `(name, isNew)` for both row types and for an empty list.
- App wiring and tmux exec follow the codebase's existing style (pure logic
  unit-tested; tmux exec not mocked).

## Out of scope (YAGNI)

- Honoring the `claude_command` alias in resume — existing `Resume` hardcodes
  `claude`; keep consistent rather than diverge. (Separate latent issue, noted
  alongside the currently-dead `resume_in_current_session` config.)
- Routing the context-menu "Resume" through the picker.
- Auto-switching to / entering passthrough on the resumed session (matches
  today's resume, which only refreshes).

## Docs to update

- `README.md` key-bindings table: `Enter` on a Closed session resumes via the
  target picker; note `f` as the fast resume-into-current path.
- In-app help (`internal/ui/help.go`, `internal/app/keys.go`) Enter description.
