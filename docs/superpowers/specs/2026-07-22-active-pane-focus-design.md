# Active-pane focus clarity

Date: 2026-07-22
Status: Approved (design), pending implementation plan

## Problem

In the three-pane layout (session list, preview, changed-files) it is not clear
which pane is currently active. Users cannot tell at a glance where keyboard
focus is.

## Current behavior (why it is unclear)

Today the "which pane is active" signal is carried entirely by a single colored
*edge* that also doubles as the separator between panes:

- **Session list focused (`ModeList`, the default):** every border is `Border`
  gray. There is no positive signal at all -- it looks identical to "nothing is
  focused." The only cue is the selected-row highlight, which is present
  regardless of focus.
- **Preview focused (`ModePassthrough`):** the *sidebar's right border* turns
  `Blue`. That line sits *between* the sidebar and the preview, so the eye
  cannot tell which side owns it. The preview has no border of its own; only its
  header bottom-border turns blue.
- **Changed-files focused (`ModeChangedFiles`):** its own left border turns
  `Blue`.

Three different, low-contrast mechanisms, and the most-used pane (the list) has
none. That is the whole problem.

## Goals

- One glance tells you which pane is active, unmistakably.
- The signal belongs to exactly one pane at a time (no shared-edge ambiguity).
- Works across all 10 supported themes with zero hardcoded colors -- every
  color derives from the active `Palette`.
- No terminal-width cost (the preview is a live tmux pane; every column matters).

## Non-goals

- No layout/geometry change (no new full borders, no width shift).
- No change to the focus model, keybindings, or Tab/Shift+Tab cycling.
- No dimming of the live preview body (raw tmux ANSI we do not own).

## Design

### One focus concept

Add a single helper `focusedPane()` returning `{PaneList, PanePreview,
PaneFiles}`, derived from `m.mode`:

- `PaneList` <- `ModeList`, `ModeSearch`, `ModeNameInput`, `ModeRenameSession`,
  `ModeContextMenu`, and any modal-overlay mode (help/stats/detail/pickers)
  where the underlying pane is the list.
- `PanePreview` <- `ModePassthrough`.
- `PaneFiles` <- `ModeChangedFiles`.

Every visual signal below keys off this single value, so the three panes can
never disagree about who is active.

### Three coordinated signals

**1. Lit title bar -- the primary, unambiguous signal.** Exactly one title is
lit at any time.

- Sidebar and changed-files: the simple title (`SESSIONS` / `CHANGED FILES`)
  renders as a solid reverse-video bar spanning the pane width when active --
  `bg = Blue`, `fg = Bg`, bold, with a leading `▸` marker. Inactive: flat,
  `fg = DimText`, no marker, no background.
- Preview header is information-dense (branch, CPU, MEM, status badge). A full
  reverse bar would clash with those colored sub-elements, so instead the
  left-most session name becomes a lit `▸ name` chip when active (`bg = Blue`,
  `fg = Bg`); inactive it is dim. Same "reverse-blue lit title" language, but it
  preserves the header's information.

**2. Active separator edge -- reinforcement, owned by its pane, no width cost.**
Swap the border *rune*, not the width: the active edge uses the thick `┃`
(`lipgloss.ThickBorder`) in `Blue`; inactive edges use the thin `│`
(`lipgloss.NormalBorder`) in `Border`. Both runes are one cell wide, so no
column is gained or lost.

Each edge belongs to exactly one pane:

- `PaneList` active -> the sidebar's right edge lights (blue + thick).
- `PaneFiles` active -> the changed-files' left edge lights (blue + thick).
- `PanePreview` active -> no edge changes. The preview owns no border in this
  layout, so preview-active is carried by its lit chip plus the neighbors
  dimming (see below).

This removes the old shared-edge ambiguity: the sidebar's right edge lights only
when the list is active, never for the preview.

**3. Dimmed inactive content -- secondary signal, status-preserving.** Panes we
render fold their text toward `DimText` when inactive.

- **Sidebar:** dim names, times, summaries, and group headers toward `DimText`,
  but **keep the status dots at their semantic colors** (green active / amber
  waiting / etc.). Rationale: in a many-sessions workflow, at-a-glance status
  across panes matters more than a fully uniform dim.
- **Changed-files:** dim all content, including the +/- stat colors.
- **Preview:** body stays bright (raw tmux ANSI; dimming is unreliable and would
  distort Claude's own colors). Only the header chip + underline read inactive.

### Palette mapping (all 10 themes)

Add semantic style variants, all built inside `buildStyles(p Palette)` from the
palette roles:

| Element                | Active            | Inactive          |
|------------------------|-------------------|-------------------|
| Pane title / chip      | `bg=Blue fg=Bg` bold, `▸` | `fg=DimText`, flat |
| Vertical separator     | `Blue` + thick `┃`| `Border` + thin `│`|
| Secondary text (dim)   | (n/a)             | `fg=DimText`      |
| Sidebar status dots    | semantic          | semantic (kept)   |

`fg = Bg` on the lit bar gives contrast on light themes too (e.g.
catppuccin-latte: near-white text on a strong blue bar). Invariant verified
across every theme: `Blue != Border`, so active vs inactive always contrast --
locked as a regression test.

## Files touched

- `internal/styles/styles.go` -- add active/inactive title-bar and separator
  style variants; build them from the palette in `buildStyles`.
- `internal/app/app.go` -- add `focusedPane()`; in `View()`, pick the sidebar /
  changed-files separator styles by focused pane and pass a focused flag into
  each pane.
- `internal/ui/sidebar.go` -- `SetFocused(bool)`; dimmed render path that
  preserves status dots; lit title bar.
- `internal/ui/changedfiles.go` -- `SetFocused(bool)`; fully dimmed render path;
  lit title bar.
- `internal/ui/preview.go` -- lit `▸ name` chip when focused; thick blue header
  underline when focused (extends the existing focused-header toggle).

## Testing

- Unit: `focusedPane()` returns the right pane for every `Mode`.
- Render diffs: sidebar and changed-files rendered with `focused=true` vs
  `false` differ in title treatment and dim; assert the sidebar's status dots
  keep their semantic colors when dimmed.
- Preview: focused render shows the lit chip + thick blue underline; unfocused
  does not.
- Palette invariant: for every theme in `styles.Themes`, `Blue != Border` (guards
  the active/inactive contrast).
- Width regression: confirm swapping thin `│` for thick `┃` and toggling the lit
  title does not change any pane's rendered width.

## Edge cases

- Modal overlays (help, stats, detail, pickers) sit on top; the underlying pane
  keeps its focus styling via the `focusedPane()` mapping (mostly `PaneList`).
- Changed-files panel closed: only list and preview participate; the mapping and
  edge rules degrade cleanly (files edge simply not drawn).
- Search / rename / name-input stack an input above the sidebar; the sidebar
  remains the focused pane (`PaneList`), so its title + edge stay lit.
