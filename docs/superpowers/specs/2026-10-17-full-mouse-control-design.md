# Full mouse control

Date: 2026-10-17

## Goal

Make the mouse a full alternative to the keyboard: select, open, and act on
sessions, drive the context menu and the popups, and trigger status-bar hints by
clicking. Extends [mouse-pane-focus](2026-07-22-mouse-pane-focus-design.md),
which only mapped a left-click's X column to a pane.

## Background: current state

`Model.handleMouse` (`internal/app/app.go`) handles pane focus only:

- **Left-click on the sidebar** calls `setFocus(ModeList)` but does not move the
  cursor to the clicked row (the earlier spec deferred row hit-testing because
  rows are variable-height and no Y-to-item map existed).
- **Right-click on the sidebar** opens the context menu for the *currently
  selected* session, ignoring the clicked row.
- **Wheel over the sidebar** is a no-op.
- **Any overlay open** (`modalActive()`) turns every mouse event into a no-op,
  so menu items can't be clicked and nothing closes on click-outside.
- `ContextMenuModel.Update` handles only `tea.KeyMsg`.
- The handler switches on the deprecated `msg.Type`.

A structural constraint shapes the design: `Model.View()` has a value receiver,
so anything a component records while rendering is discarded with the copy.
Hit maps therefore must be built in code that runs on the real model, or
recomputed on demand from the same pure layout function `View()` uses.

## Scope

**In scope:** row hit-testing in the sidebar, click/double-click/right-click on
rows and group headers, wheel selection in the list, context-menu item clicks,
click-outside-to-close for every overlay, row clicks and wheel in the theme,
directory and resume pickers, stats filter clicks, and status-bar hint clicks.

**Out of scope (explicit):** forwarding mouse events into the Claude pane during
passthrough; clicking rows in the changed-files pane (its wheel keeps working);
mouse support in the inline name/rename inputs and the kill confirmation (they
keep ignoring the mouse).

## Behavior

Clicks act on `tea.MouseActionPress` (wheel ticks arrive as presses), so a
press+release pair is handled once and drag motion is ignored; the release is
only used to finish a long press. Buttons are read from `msg.Button`.

### Session list

| Input | Target | Action |
|---|---|---|
| Left press | session row | select it, focus the list (leaving passthrough / changed-files focus) |
| Left press ×2 within 400 ms | same session row | as Enter: passthrough for active, resume picker for closed |
| Left press | group header | select it and toggle collapse (same as Enter on a header) |
| Left press | blank / pane title | focus the list (unchanged) |
| Right press | session row | select it, then open the context menu at the click |
| Left press held ≥ 450 ms, released on the same row | session row | same as right press, menu at the press position |
| Right press | header / blank | nothing |
| Wheel up/down | anywhere over the list | move the selection by one, like `k`/`j` |

- Double-click identity is the hit item (session `Key()` + tmux target), not the
  raw Y: after the first click the selected row grows to 4-5 lines and the list
  may scroll, so a second press at the same Y can land on a different row; it
  then degrades to a plain select.
- Long press is the right-click fallback for a regular tmux pane, where the
  user's root `MouseDown3Pane` binding swallows right-click. It is detected on
  release: press and release on the same session row, at least
  `longPressThreshold` (450 ms) apart, with no other button event in between
  (any press, wheel included, cancels it). The press itself still selects the
  row, as a short click does; a long press clears the double-click tracker so
  it never pairs with the next click. Time comes from an injectable clock so
  tests don't sleep.
- During search the list stays in search mode: a click selects the result, a
  double-click acts as search Enter, wheel acts as search up/down. Header clicks
  and right-clicks are ignored while searching (search expands every group, and
  the context menu's close path would leave the search input orphaned).
- Wheel while in passthrough first drops focus to the list, so the forwarded-key
  target never silently changes under the user.

### Context menu

| Input | Action |
|---|---|
| Left press on an item | run it (same path as Enter) |
| Left press on border / separator | nothing |
| Left or right press outside | close |
| Right press on another session row | close, then reopen for that row |

### Popups

| Popup | Inside | Outside |
|---|---|---|
| Help, Detail | nothing | close |
| Stats | click a filter label (All time / This week / Today) selects it | close |
| Theme picker | click a row selects + previews it; click the selected row applies | cancel (restores theme) |
| Directory / resume picker | click a row selects it; click the selected row confirms | cancel |

Wheel scrolls the cursor in the three pickers and is ignored by the other
popups. Clicking an already-selected row confirms, so a double-click confirms.

### Status bar

Clicking a hint (key badge or its description) on the bottom line sends that
hint's key:

- List hints (shown in list and changed-files modes) dispatch through the
  list-mode key handler.
- Passthrough, search, context-menu, theme-picker, detail and stats hints
  dispatch through `handleKey`, which routes by mode.
- Other modals show the list hints underneath (a pre-existing status-bar quirk),
  so a click there is treated as a click outside the popup.
- `j/k` is not clickable; `any key` sends Esc.

### Overlay layering

Overlays are hit-tested top-down in View's compositing order (context menu,
help, detail, resume picker, dir picker, theme picker, stats). Mouse input
never reaches panes underneath an open overlay.

## Implementation

- `internal/ui/sidebar.go`: `syncViewport` records each content line's owning
  flat-item index (`-1` for the blank line before a non-first group header).
  `HitTest(x, y)` takes a sidebar-local row (0 = top of the SESSIONS title,
  which wraps to two lines on very narrow panes), adds `vp.YOffset`, and
  returns a `SidebarHit` (kind, flat index, session, group).
  `MoveCursor(delta)` and `ToggleGroupAtCursor()` are extracted from `Update` so
  keyboard and mouse share them.
- `internal/ui/overlay.go`: `OverlayRect` returns the rect `PlaceOverlay` uses,
  so click-inside tests match the drawn popup.
- `internal/ui/contextmenu.go`: View is split into a box render plus a clamped
  `Rect()`; `ItemAt(x, y)` and `SetCursor` support clicks.
- `internal/ui/statusbar.go`: hints are laid out by one function that returns
  the rendered line and each hint's column span; `HintAt(x)` reads the spans.
- Pickers and stats gain `RowAt(y)` / `SetCursor(i)` / `FilterAt(x, y)` helpers
  that mirror their View layout.
- `internal/app/mouse.go` (new): `handleMouse` and its helpers move out of
  `app.go`. Terminal Y maps to sidebar Y by subtracting the title bar height and
  any stacked search input; the hit test runs on a sidebar copy prepared the
  way View() prepares it (focus state, search height) so it matches the
  screen. Shared bodies are extracted from the key handlers:
  `afterSelectionChange` (list post-navigation), `runContextMenuSelection`.
- Status-bar labels map to key messages via a reverse table of bubbletea key
  names; single runes stay case-sensitive.

## Testing

- `internal/ui`: table-driven sidebar `HitTest` across the pane title, group
  headers, the blank line between groups, normal rows, the multi-line selected
  row, and a scrolled viewport; context-menu `ItemAt` (items, separators,
  border, outside); status-bar `HintAt`.
- `internal/app`: click-select, double-click (active → passthrough), group
  header toggle, right-click opens the menu for the clicked row, menu item click
  runs its action, click-outside closes the menu and help, wheel moves the
  selection, status-bar hint click triggers its action, picker row click /
  confirm / cancel, stats filter click, long press (held / short / released
  elsewhere / interrupted) and long press not arming double-click, and clicks
  at the row where View() actually draws a session in list, passthrough and
  search modes. Existing `mouse_focus_test.go` cases switch to
  `Button`/`Action`.

Test sessions use tmux targets inside naviClaude's own window (or none) so
`selectPreviewSession` never shells out to resize real tmux windows.
