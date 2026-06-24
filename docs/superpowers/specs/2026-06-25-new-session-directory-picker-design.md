# New-session directory picker

Date: 2026-06-25

## Problem

Creating a session gives no control over its working directory:

- `n` (new Claude session in the current tmux session) always opens in the CWD
  of the highlighted session row (`app.go` `createNewSession` → `sel.CWD`).
- `N` (new tmux session) always opens in `new_session_dir` from config
  (default `~`), and only prompts for a session *name*.

There is no way to pick a different directory interactively.

## Goal

A single interactive directory picker, triggered by both `n` and `N`, that is
**a fuzzy filter and a filesystem navigator at the same time** (broot/telescope
style). It is seeded from the user's `zoxide` frecent directories so known
projects are one fuzzy query away, and it also drills into the filesystem tree
for arbitrary directories.

## UX

The picker is a centered overlay popup (same rendering path as the theme
picker): a title, a query input, a scrollable candidate list, and a key-hint
footer.

```
 New session directory          base: ~/personal/git
 > navi
 ----------------------------------------------------
 . ~/personal/git              (current — Enter picks)
 * ~/personal/git/naviClaude   (zoxide)
 D naviClaude                  (subdir)
 ...
 ----------------------------------------------------
 type=filter   up/down move   ->/Tab enter dir   <- parent   Enter select   Esc cancel
```

Keys:

- typing — fuzzy-filters the candidate pool (reuses `sahilm/fuzzy`)
- up/down, ctrl+p/ctrl+n — move the cursor
- right / Tab — descend into the highlighted directory (it becomes the new
  `base`; list repopulates; query clears)
- left — ascend to the parent of `base`
- Enter — select the highlighted directory as the session's working directory
- Esc — cancel, return to the list with no session created

The picker opens with `base` set to the flow's current default and that default
preselected at the top, so a single Enter reproduces today's behavior.

## Candidate pool

For the current `base`, the pool is the de-duplicated (by absolute path) union,
in this order:

1. `base` itself (the "current" entry; lets Enter pick it immediately)
2. immediate subdirectories of `base` (sorted; hidden `.`-dirs skipped)
3. `zoxide` frecent directories (`zoxide query -l`, in zoxide order)
4. directories of currently open sessions

When the query is non-empty the order is replaced by fuzzy score.

`zoxide` is optional: if the binary is absent the query function returns nothing
and the picker still works from the filesystem + session dirs.

## Architecture

- New `internal/ui/dirpicker.go` — `DirPickerModel`, modeled on
  `ThemePickerModel` (overlay with `Show/Hide/IsVisible/SetSize/Update/View`)
  but with a `textinput` for filtering and its own cursor. The external
  dependencies — zoxide query and subdir listing — are injectable function
  fields so the pure logic (candidate building, fuzzy filtering, navigation) is
  unit-testable without touching the real filesystem or zoxide.
- New `ModeDirPicker` in `internal/app/modes.go`.
- `app.go`:
  - `n` flow: derive the target tmux session and default dir as today, then open
    the picker (base = default dir) instead of creating immediately. On Enter,
    create the Claude window in the chosen dir via the existing
    `CreateNewWithTarget`.
  - `N` flow: open the picker (base = `new_session_dir`/home) first; on Enter,
    store the chosen dir and continue into the existing name-input flow, which
    then creates the new tmux session there.
  - A small `pendingDirAction` (which flow + target tmux session) carries intent
    from selection to creation.
  - Render the picker via `PlaceOverlay` in `View`; route keys via a
    `handleDirPickerKey`; call `SetSize` alongside the other overlays on resize.

## Testing

Unit tests for the pure logic in `dirpicker_test.go`:

- `buildCandidates`: dedup by path, base-first ordering, hidden-dir skipping,
  zoxide + session-dir inclusion (using injected fakes + a `t.TempDir()` tree).
- fuzzy filtering returns expected ordering for a query.
- navigation: descend sets base to the highlighted dir; parent ascends; tilde
  expansion.
- `Activate`/selection round-trip.

App wiring is verified by building the binary and exercising `n`/`N` manually.

## Out of scope (YAGNI)

Creating new directories from the picker, multi-select, a hidden-dir toggle, and
persisting a "last used" directory. Can be added later if wanted.
