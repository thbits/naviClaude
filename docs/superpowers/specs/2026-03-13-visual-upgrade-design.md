# naviClaude Visual Upgrade -- Design Spec

## Overview

Three-phase visual upgrade to naviClaude's TUI. Each phase is independently shippable and builds on the previous. The goal is to make the app feel premium while preserving the existing keyboard-driven workflow and information density.

## Phase 1: Style Polish

Pure visual refinements. No new state, no new dependencies, no new tick subscriptions.

### Changes

**1. Gradient selection bar**
- Replace solid blue `BorderForeground(ColorBlue)` on `SidebarItemSelected` with a blue-to-purple vertical gradient.
- Lipgloss v1 does not support `BorderForegroundBlend` -- implement by rendering two half-block characters (`\u2580` upper / `\u2584` lower) in blue and purple stacked vertically, replacing the current `SelectionIndicator` border approach with a custom-rendered left column.
- Fallback: if gradient rendering proves impractical in v1, use alternating blue/purple quarter-block chars for a dithered gradient effect. Simplest fallback: just use purple (`ColorPurple`) as the border color for a distinct but non-gradient look.

**2. Directional selection background fade**
- Current: flat `ColorSelection` (#2a3a5e) background.
- After: the selected item row still uses `ColorSelection` as background. The "fade" effect is achieved by rendering the summary line (line 2) with a slightly dimmer background variant. This creates a subtle top-to-bottom depth without needing horizontal gradients (which Lipgloss v1 cannot do per-cell).
- Implementation: add `ColorSelectionDim` to the palette (e.g., #243350) used for the summary line of selected items.

**3. Gradient title text**
- "naviClaude" in the title bar rendered with alternating blue/purple characters.
- Implementation: iterate over the string, applying `ColorBlue` to even-index chars and `ColorPurple` to odd-index chars. This creates a character-level gradient effect within terminal constraints.

**4. Letter-spaced SESSIONS header**
- Insert spaces between characters: "S E S S I O N S".
- Pure string manipulation in `sidebar.go` View().

**5. Dot separators**
- Replace pipe `|` with centered dot `\u2022` (bullet) in:
  - Status bar separator (`StatusBarSep`)
  - Preview header separator (`PreviewSep`)
- Softer visual rhythm.

**6. Improved item spacing**
- Add 1-line empty gap between group headers and the first item when a group transitions to the next.
- Slightly increase vertical breathing room in session items.

### Files touched

- `internal/styles/styles.go` -- new `ColorSelectionDim`, updated `StatusBarSep` char, updated `PreviewSep` char
- `internal/styles/themes.go` -- add `SelectionDim` to `Palette` struct, populate in all 10 themes
- `internal/ui/sidebar.go` -- letter-spaced header, gradient left bar rendering, selection dim bg on summary line
- `internal/ui/statusbar.go` -- dot separator
- `internal/ui/preview.go` -- dot separator in header
- `internal/app/app.go` -- gradient title text rendering

## Phase 2: Rich Data Display

New session fields and expanded selected-item rendering. No new external dependencies.

### Changes

**1. Activity sparkline (selected item only)**
- Show a 10-bar sparkline using block characters (`\u2581` through `\u2588`) below the summary line of the selected session.
- Each bar represents a time bucket of recent message activity.
- Color: bars scale from `ColorGray` (low) through `ColorBlue` -> `ColorPurple` -> `ColorGreen` (high).
- Width-aware: hide entirely below 35-char sidebar width. Show 6 bars at 35-45 chars. Full 10 bars above 45.
- Append message count label: "12 msgs" in `ColorGray`.

**2. Token usage mini-bar (selected item only)**
- 3px-tall progress bar showing context window utilization.
- Rendered using block characters: filled portion in `ColorBlue`/`ColorPurple`, empty in `ColorBorder`.
- Width: scales to available sidebar width minus padding (max ~20 chars).
- Label: "35K ctx" in `ColorGray`.

**3. Richer preview header**
- Add status text badge: "ACTIVE" / "WAITING" / "IDLE" in corresponding status color, bold.
- Add uptime: hourglass char + formatted duration (e.g., "23m", "1h12m").
- Add message count.
- Use dot separators (from Phase 1).

**4. Configurable density**
- New config option: `sidebar_density: compact | normal | detailed`
  - `compact`: 1 line per session (icon + name + time, no summary)
  - `normal`: 2 lines per session (current behavior + Phase 1 polish)
  - `detailed`: sparkline + token bar on ALL items (not just selected)
- Default: `normal`. In `normal` mode, sparkline/token bar are shown only on the selected item. In `detailed` mode, all items show sparkline/token bar. In `compact` mode, no summary line or sparkline/token bar are shown (Phase 1's selection dim background is absent since there is no line 2; the single selected line uses `ColorSelection` throughout).

### New session fields

```go
type Session struct {
    // ... existing fields ...
    RecentActivity []int  // 10 buckets of message counts for sparkline
    TokensUsed     int    // approximate tokens consumed in context
    ContextLimit   int    // context window size (e.g., 200000)
    MessageCount   int    // total messages in conversation
    Uptime         time.Duration // time since session started
}
```

### Data population strategy

**`RecentActivity []int` (sparkline data):**
- Computed lazily: only for the currently selected session, not all sessions on every scan.
- On selection change, read the session's JSONL file and bucket message timestamps into 10 time slots covering the last hour (6-minute buckets).
- Cache the result on the `Session` struct; invalidate when `LastActivity` changes.
- For closed sessions, compute once from the full JSONL on first selection (immutable data).

**`TokensUsed int` / `ContextLimit int`:**
- `ContextLimit`: hardcoded per model name. Map of known model -> context size (e.g., "claude-opus-4-6" -> 1000000, "claude-sonnet-4-6" -> 200000). Default 200000 for unknown models.
- `TokensUsed`: estimate from JSONL by summing content lengths and dividing by 4 (rough chars-to-tokens ratio). Computed lazily alongside `RecentActivity` on selection change.

**`MessageCount int`:**
- Already partially available: `detail.go` computes this asynchronously. Move that logic into the detector's scan for the selected session, or count JSONL lines during the lazy sparkline computation.

**`Uptime time.Duration`:**
- For active sessions: `time.Since(firstTimestamp)` where `firstTimestamp` is the first entry in the JSONL file. Read once on first selection, cache.
- For closed sessions: `lastTimestamp - firstTimestamp` from the JSONL file.

**Performance:** The lazy/selected-only approach means at most one JSONL file read per selection change, not per refresh cycle. This is the same cost as the existing detail popup.

### Files touched

- `internal/session/session.go` -- new fields
- `internal/session/detector.go` -- lazy population for selected session
- `internal/ui/sidebar.go` -- sparkline + token bar rendering for selected item, density-aware rendering
- `internal/ui/preview.go` -- richer header with status badge, uptime, message count
- `internal/styles/styles.go` -- new `SparklineBar`, `TokenBarFilled`, `TokenBarEmpty`, `StatusBadgeActive`, `StatusBadgeWaiting`, `StatusBadgeIdle` styles
- `internal/styles/themes.go` -- new styles in `ApplyTheme()`
- `internal/config/config.go` -- `SidebarDensity` field

## Phase 3: Motion & Life

Animated elements using Bubble Tea's tick system and the bubbles/spinner component.

### Changes

**1. Breathing status dots**
- Active sessions: icon alternates between bright green (`ColorGreen`) and a dimmer green variant on an ~800ms tick cycle.
- Waiting sessions: icon alternates between bright amber (`ColorAmber`) and dimmer amber on a ~1200ms tick cycle.
- Idle/Closed: static, no animation (provides visual anchor).
- Implementation: a single `breathingTickMsg` on a 400ms interval. A frame counter determines which icons are bright vs dim (active toggles every 2 ticks = 800ms, waiting every 3 ticks = 1200ms).

**2. Loading spinner**
- Replace "Loading..." text with `bubbles/spinner` (Dot style) during:
  - Initial session scan
  - Async data loads (stats computation, detail popup)
- Spinner runs on its own tick (built into the component).
- Style: `ColorBlue` foreground.

**3. Status transition flash**
- Build flash infrastructure from scratch. Note: `SidebarWaitingFlash` exists as a style stub in `styles.go` but is never used in any rendering or app logic. This is new functionality.
- Flash types by transition:
  - active->waiting: amber flash
  - active->idle: gray flash
  - waiting->active: green flash
  - any->closed: dim flash
- Flash rendering: on status change detection, the session row renders one frame with inverse-color style (foreground/background swap of the target status color). The flash is visible for exactly one render cycle.
- Implementation: compare `prevStatuses[sessionID]` with current status on each `sessionRefreshMsg`. If changed, add to `flashTargets` with `time.Now()`. In `renderSessionItem`, if session is in `flashTargets` and elapsed < 200ms, use the flash style. The 200ms preview refresh guarantees at least one render with the flash visible.
- Track `previousStatus` per session to detect transitions.

### New state

```go
type Model struct {
    // ... existing fields ...
    breathingFrame int           // increments on each breathingTickMsg
    spinner        spinner.Model // bubbles/spinner for loading states
    prevStatuses   map[string]session.SessionStatus // track status transitions
    flashTargets   map[string]time.Time             // sessions currently flashing
}
```

### Files touched

- `internal/ui/sidebar.go` -- breathing dot rendering, transition flash rendering
- `internal/app/app.go` -- breathingTickMsg subscription, spinner model, prevStatuses tracking
- `internal/styles/styles.go` -- `ColorGreenDim`, `ColorAmberDim` variants, flash styles for each transition
- `internal/styles/themes.go` -- dim color variants in palette and `ApplyTheme()`

## Design Decisions

**Why expand-on-select for sparklines/token bars (Phase 2)?**
Showing rich data on all items would double the vertical space per item, halving the visible session count. Expand-on-select preserves list density while giving full detail where the user is looking. The `detailed` density config lets power users override this.

**Why character-level gradient instead of Lipgloss gradient API (Phase 1)?**
naviClaude uses Lipgloss v1 (github.com/charmbracelet/lipgloss, not charm.land/lipgloss/v2). The `BorderForegroundBlend()` and `Blend1D/2D` APIs are v2-only. Character-level color alternation achieves a similar visual effect within v1 constraints.

**Why a single breathing tick instead of per-session timers?**
One global tick at 400ms is simpler and cheaper than N per-session timers. The frame counter modulo math determines which sessions are bright vs dim on each tick.

## Graceful Degradation

At very small terminal sizes (below 60x15), Phase 1-3 additions degrade:
- Below 80 cols: sparklines hidden, token bar hidden
- Below 60 cols: letter-spacing removed from SESSIONS header, gradient title falls back to plain blue
- Below 15 rows: only compact density is used regardless of config (auto-override)
- The title gradient is intentionally static (not animated) to avoid visual noise in the chrome area

## Non-Goals

- No new external dependencies beyond what's already in go.mod (bubbles/spinner is in charmbracelet/bubbles, already imported).
- No changes to the theme picker, help overlay, stats overlay, or context menu.
- No changes to keybindings or input handling.
- No changes to the config file format beyond adding `sidebar_density`.
