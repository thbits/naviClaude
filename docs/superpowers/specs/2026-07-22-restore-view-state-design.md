# Restore View State on Reopen

## Problem

When naviClaude reopens, it always focuses the first session (alphabetical) and
resets group open/closed state. Users who work across many sessions lose their
place on every launch. We want optional restoration of two pieces of UI state so
the app feels consistent across restarts:

1. The session that was focused when the app last closed.
2. Which session groups (e.g. `default`, `hermes`) were open or closed.

If the previously focused session no longer exists, the app falls back to
focusing the first session (current behavior).

## Scope

In scope (each independently opt-in):

- Restore the last focused session.
- Restore manually-toggled group open/closed state.

Explicitly out of scope (deliberately not restored):

- Passthrough / which pane had focus. Reopening straight into a live pane would
  forward stray keystrokes and is jarring; the app always starts in list mode.
- Preview scroll position, search query/mode, open modals/pickers. Ephemeral.
- Changed-files (right) panel open/closed. Always starts closed, as today.

## Configuration

Two granular flags on `config.Config`, both `bool`, both default `false`, so
existing behavior is unchanged until a user opts in:

| yaml key               | field                | effect                                  |
| ---------------------- | -------------------- | --------------------------------------- |
| `focus_last_session`   | `FocusLastSession`   | re-select the last focused session      |
| `remember_group_state` | `RememberGroupState` | restore manually-toggled group state    |

Both are added to `DefaultConfig()` (false is the zero value; no `sanitizeConfig`
handling needed since bool has no invalid range).

## State store

New file `internal/session/viewstate.go`, mirroring `AliasStore`
(`session-names.json`) in structure and error handling.

- Path: `~/.config/naviclaude/view-state.json` via `DefaultViewStatePath()`.
- On-disk shape:

  ```json
  {
    "last_session_id": "…",
    "collapsed_groups": { "hermes": true, "default": false }
  }
  ```

- Type:

  ```go
  type ViewState struct {
      LastSessionID   string          `json:"last_session_id"`
      CollapsedGroups map[string]bool `json:"collapsed_groups"`
  }

  type ViewStateStore struct {
      path    string
      mu      sync.RWMutex
      state   ViewState
      loadErr error
  }
  ```

- API: `NewViewStateStore(path string) *ViewStateStore` (empty path -> default,
  loads on construction), `Get() ViewState` (returns a copy), `Save(ViewState) error`
  (writes atomically like `AliasStore`, creating parent dirs).
- `collapsed_groups` holds only the groups the user manually toggled, mapped to
  their collapsed value.

### Write vs. apply policy

To avoid merge logic: on quit we **always write** the current view state (both
fields). On startup we **apply each field only if its toggle is on**. Writing the
true current state is never wrong; a disabled feature simply ignores its field on
read. A stored `last_session_id` with `focus_last_session=false` is inert.

## Sidebar API

Two small methods on `SidebarModel` (`internal/ui/sidebar.go`):

- `ToggledGroups() map[string]bool` — returns `{groupName: collapsed}` for every
  group in `userToggled`. Read at quit.
- `SeedToggledGroups(groups map[string]bool)` — for each entry sets
  `userToggled[name] = true` and `collapsed[name] = value`. Called once in `New()`
  before the first `SetSessions`.

Because seeding populates `userToggled`, `rebuildGroups` honors these groups and
auto-collapse (`collapse_after_hours`) leaves them alone — identical to how a live
manual toggle behaves. Stale entries for vanished tmux sessions are harmless:
`rebuildGroups` only iterates existing groups, so unused keys just sit in the maps.

## App wiring

`Model` (`internal/app/app.go`) gains:

- `viewState *session.ViewStateStore`
- `restoredLastSession bool` — one-shot guard for session restore.

### `New()`

- Construct `viewState = session.NewViewStateStore("")`.
- If `cfg.RememberGroupState`, call `m.sidebar.SeedToggledGroups(vs.CollapsedGroups)`
  before the first `SetSessions` (the sidebar is already created above; seeding
  happens right after, alongside the existing `SetCollapseAfterHours` / sort-order
  wiring).

### Session restore

Add a helper `maybeRestoreLastSession()` that, when `cfg.FocusLastSession` and
`!m.restoredLastSession` and a non-empty stored ID exists, calls
`m.sidebar.SelectByID(id)`. On success it sets `restoredLastSession = true`,
refreshes the preview (`selectPreviewSession`) and metrics
(`reloadMetricsForSelection`) for the restored session, and returns any resulting
`tea.Cmd`.

Call sites:

- `activeSessionsMsg` — after the sidebar receives the active list (the stored
  session may be active).
- `historySessionsMsg` — after the combined list is set (the stored session may be
  closed, only present after history loads).

The one-shot flag is also set to `true` the first time the user navigates the list
(the default nav branch in `handleListKey`), so a late history-driven restore never
yanks a cursor the user has already moved. If `SelectByID` never succeeds (session
gone), the default first-session cursor stands — the existing fallback behavior.

### Save on quit

Add `persistViewState()`:

```go
func (m *Model) persistViewState() {
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

Called at the user-initiated quit choke points that already call
`restorePreviewedPane()`:

- list-mode quit (`handleListKey`, `m.keys.Quit`)
- changed-files-mode quit (`handleChangedFilesKey`, `m.keys.Quit`)
- global Ctrl+C (`handleKey`)
- `jumpToPane` (saves the session being jumped into — the truest "last one I was on")
- resume-jump path (`resumeMsg` with `pendingResumeJump`)

## Testing

- `internal/config/config_test.go`: `focus_last_session` and
  `remember_group_state` parse from YAML; both default `false` when absent.
- New `internal/session/viewstate_test.go`: `Save`/`Get` roundtrip (both fields);
  missing file returns a zero `ViewState` with nil `loadErr`; corrupt file surfaces
  via `loadErr`.
- `internal/ui/sidebar_test.go` (or focus test): `SeedToggledGroups` makes a group
  render collapsed/expanded per seed and survives auto-collapse; `ToggledGroups`
  reflects a manual toggle.
- App-level (mirroring `internal/app/focus_test.go`): with `FocusLastSession=true`
  and a stored ID matching a session, that session becomes selected after load;
  with a stored ID that no longer exists, the first session stays selected.

## Files touched

- `internal/config/config.go` (+ test) — two new fields.
- `internal/session/viewstate.go` (new) + `viewstate_test.go` (new).
- `internal/ui/sidebar.go` (+ test) — `ToggledGroups`, `SeedToggledGroups`.
- `internal/app/app.go` (+ test) — store field, seed in `New()`, restore helper +
  call sites, `persistViewState` + quit call sites, one-shot flag.
