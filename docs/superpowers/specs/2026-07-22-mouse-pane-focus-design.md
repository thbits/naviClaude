# Mouse pane-focus

Date: 2026-07-22

## Goal

Left-clicking a layout pane gives that pane keyboard focus, using the same
focus model the keyboard already drives. Complements
[active-pane-focus](2026-07-22-active-pane-focus-design.md).

## Background: current state

Mouse reporting is already enabled (`tea.WithMouseCellMotion()` in
`cmd/naviclaude/main.go`) and `Model.handleMouse` (in `internal/app/app.go`)
exists, but it predates the three-pane focus system (`setFocus`,
`focusedPane`, `nextFocusMode` in `internal/app/focus.go` and `app.go`) and is
mostly non-functional:

- **Left-click on the sidebar** delegates to `sidebar.Update(msg)`, but
  `SidebarModel.Update` only handles `tea.KeyMsg` — the mouse message is a
  no-op, so no selection or focus change happens.
- **Left-click outside the sidebar** always enters passthrough via a manual
  `m.mode = ModePassthrough`, bypassing `setFocus`. It does not recognize the
  right changed-files pane, so clicking there wrongly enters preview
  passthrough.
- Because clicks set `m.mode` directly instead of calling `setFocus`, the
  transition side effects the keyboard path runs (passthrough enter/leave,
  changed-files reload, tmux pane resize) are skipped, so mouse-driven focus
  drifts from keyboard-driven focus.

## Scope

**In scope:** pane-level focus on left-click, routed through `setFocus`.

**Out of scope (explicit):** row-level click-to-select inside a pane (moving
the sidebar cursor or file selection to the exact clicked row). Rows are
variable-height and no Y-to-item mapping exists; row selection stays on
`j`/`k`. May be revisited later.

## Behavior

Left-click maps the click's X column to a pane and calls `setFocus`:

| Region | Condition | Action |
|---|---|---|
| Left sidebar | `X < sidebarWidth()` | `setFocus(ModeList)` |
| Center preview | `sidebarWidth() <= X < sidebarWidth() + previewWidth` | `setFocus(ModePassthrough)` if the selection can passthrough, else no-op |
| Right changed-files | `X >= sidebarWidth() + previewWidth` and `rightPanelOpen` | `setFocus(ModeChangedFiles)` |

- "Can passthrough" mirrors `nextFocusMode`: selection is non-nil, not
  `StatusClosed`, and has a non-empty `TmuxTarget`. If a click lands on the
  preview while the selection cannot passthrough (e.g. a closed session),
  focus is left unchanged rather than forced elsewhere.
- `previewWidth = width - sidebarWidth() - rightSidebarWidth()`, matching
  `View()`. When the right panel is closed, `rightSidebarWidth()` is 0, so the
  preview band extends to the right edge and the changed-files region does not
  exist.

**Right-click:** unchanged — right-click on the sidebar opens the context menu.

**Wheel:** preview scroll is unchanged. Wheel over the right changed-files
region routes to the changed-files pane when the panel is open. Wheel over the
sidebar remains a no-op (the sidebar model does not scroll on wheel; unchanged).

**Modal guard:** when any overlay, picker, or inline input is active — help,
stats, theme picker, directory picker, resume picker, context menu, name
input, rename input — `handleMouse` ignores clicks and returns without
changing pane focus. This prevents a click from silently re-focusing a pane
hidden underneath a modal. Search (`ModeSearch`) stacks above the list and
keeps list focus; a click in the sidebar region while searching focuses the
list normally.

## Implementation

Single file: `internal/app/app.go`, rewriting `handleMouse`.

1. Add an early return when a modal/picker/input is active (reuse the existing
   `IsVisible()` / `IsActive()` predicates already used in `View()` and the
   fall-through forwarding at the end of `Update`).
2. Compute `sidebarWidth`, `rightWidth`, and the preview band from the same
   helpers `View()` uses.
3. For `tea.MouseLeft`, select the pane by X and call `setFocus(...)`,
   returning its command.
4. For `tea.MouseRight`, keep the sidebar context-menu behavior.
5. For wheel, keep preview scrolling and add changed-files routing when the
   click is in the right region and the panel is open.

`setFocus` already takes `*Model`; `handleMouse` has a value receiver, so it
follows the existing pattern of calling `setFocus` on the addressable local
copy (as `handleListKey` etc. do) and returning its command.

## Testing

Add table-driven tests in a new `internal/app/mouse_focus_test.go` (mirroring
`focus_test.go`):

- Click in each region sets the expected mode/focus, at representative widths
  with the right panel both open and closed.
- Click on the preview with a closed selection leaves focus unchanged.
- Click while a modal is active leaves mode unchanged.
- Focused-pane assertion after each click via `focusedPane()`.

No new dependencies. Manual check: run in tmux, click each pane, confirm the
focus border/header matches the keyboard result.
