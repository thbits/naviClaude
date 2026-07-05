# New Claude session opens in the selected tmux-session group

## Problem

When the sidebar cursor is on a group header (a tmux session, e.g. `naviClaude`
or `default`) and the user creates a new Claude session, the session should open
inside **that** tmux session. It does when the group is expanded, but not when
it is collapsed.

`createNewSession` (internal/app/app.go) derives the target tmux session by
scanning the group header's visible child sessions in `FlatItems()`. naviClaude
auto-collapses idle groups, and collapsed groups contribute no child items to
`flatItems` (internal/ui/sidebar.go `rebuildFlatItems`). So on a collapsed
header:

1. `SelectedSession()` returns nil (cursor is on a header).
2. The child scan starts at `items[cursor+1:]`, which is the next group header,
   and breaks immediately -- `tmuxSess` stays empty.
3. The code falls back to `m.currentTmuxSession`, so the new Claude session
   opens in naviClaude's own tmux session instead of the selected one.

## Fix

Groups are keyed by tmux session name (`rebuildFlatItems` / `groupMap[s.TmuxSession]`),
so the **group header name is the tmux session name**. Use it directly instead
of scanning for a visible child:

- If the cursor is on a group header and the group is not the `"Closed"` group,
  set `tmuxSess = SelectedGroupName()`.
- Derive `cwd` from `GroupSessions(name)`'s first session when available. This
  reads `m.groups`, not `flatItems`, so it works even when the group is
  collapsed. Fall back to the home directory otherwise.
- Leave the existing fallback chain (`SelectedSession()` first, then
  `currentTmuxSession`) intact for every other case, including the `"Closed"`
  group.

No change to `CreateNewWithTarget`: it already opens a new window in whatever
tmux session name it is given.

## Scope

- `createNewSession` in internal/app/app.go.
- A test covering the collapsed-group-header case (target resolves to the
  selected group's tmux session, not naviClaude's own).
