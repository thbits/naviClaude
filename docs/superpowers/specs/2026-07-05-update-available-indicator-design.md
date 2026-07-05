# Update-Available Indicator — Design

**Date:** 2026-07-05
**Status:** Approved

## Goal

Show a plain `update available` label next to the version string in the
bottom-right status bar when a newer GitHub release exists.

## Decisions

- **Source of truth: GitHub Releases API**, not Homebrew. goreleaser publishes
  releases to GitHub and the Homebrew tap formula is generated from them, so
  GitHub is upstream. It is also install-method agnostic (brew, manual, `go
  install`) and needs no subprocess.
- **Timing:** check once on startup, asynchronously (never blocks the UI).
- **Caching:** 6-hour TTL. Within 6h of the last check, use the cached result
  and make no network call.
- **Indicator:** plain, display-only `update available` label. No target
  version, no keybinding/action.
- **Configurable:** `check_for_updates` config option, default `true`. Users can
  opt out for privacy/offline use.
- **Failures are silent:** any error, timeout, non-200, unparseable version, or
  `dev` build shows no label — never an error in the status bar.

## Architecture

### 1. `internal/update` package (new)

Self-contained; knows nothing about the UI.

```go
package update

// Check returns the latest release version and whether it is newer than
// current. Returns ("", false) on any error, for "dev" builds, or when
// current >= latest.
func Check(ctx context.Context, current string) (latest string, available bool)
```

- GET `https://api.github.com/repos/thbits/naviClaude/releases/latest`, read
  `tag_name`.
- `http.Client` with ~3s timeout.
- Cache file at `os.UserCacheDir()/naviclaude/update-check.json` holding
  `{checked_at, latest_tag}`. If younger than 6h, return from cache with no
  network call. Refresh timestamp + tag after a successful fetch.
- Semver-aware compare tolerant of a `v` prefix. `current == "dev"` or an
  unparseable current/latest returns `("", false)`.
- All failure paths return `("", false)`.
- Unit-testable against an `httptest.Server` and a temp cache dir.

### 2. Elm loop wiring (`internal/app/app.go`)

Follows the existing async-command pattern (`refreshActiveCmd` →
`activeSessionsMsg`):

- New `updateAvailableMsg{ latest string }`.
- New `checkUpdateCmd()` `tea.Cmd` running `update.Check` off the render path,
  emitting the message only when `available`.
- Fired from `Init()`'s `tea.Batch`, gated on `m.cfg.CheckForUpdates`.
- `Update()` handles `updateAvailableMsg` by calling
  `m.statusbar.SetUpdateAvailable(true)`.

### 3. Status bar (`internal/ui/statusbar.go`)

- Field `updateAvailable bool` + `SetUpdateAvailable(bool)` setter.
- In `View()`, when true, render the right side as
  `version + "  " + styles.StatusBarUpdate.Render("update available")`. The
  existing gap math uses `lipgloss.Width(right)`, so alignment just works.

### 4. Config (`internal/config/config.go`)

- Add `CheckForUpdates bool `yaml:"check_for_updates"`` to `Config`.
- Default `true` in `DefaultConfig()`.
- No `sanitizeConfig` handling: `Load` unmarshals YAML on top of
  `DefaultConfig()`, so an absent key keeps `true` and an explicit `false`
  disables — same mechanism as `ResumeInCurrent`. Adding a bool coercion would
  wrongly force the value.

### 5. Styles (`internal/styles/styles.go`)

- Add `StatusBarUpdate` style using the theme accent/warning color, defined
  per-theme alongside `StatusBarVersion`.

## Testing

- `internal/update`: table tests for version compare (v-prefix, dev, equal,
  older, newer, garbage); cache hit/miss/expiry against temp dir; HTTP success
  and failure paths against `httptest.Server`.
- Config: `check_for_updates` absent → `true`; explicit `false` → `false`.
- Status bar: `View()` renders the label only when `updateAvailable` is true.

## Out of scope

- Auto-updating / self-update.
- Opening the release page or running brew from the TUI.
- Periodic re-checks while running.
