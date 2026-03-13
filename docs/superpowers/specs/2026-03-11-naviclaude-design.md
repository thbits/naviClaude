# naviClaude — Design Spec

A Bubble Tea TUI for managing Claude Code sessions across tmux, with live terminal preview, full passthrough interaction, and session history.

## Problem

Running many simultaneous Claude Code sessions across different tmux sessions becomes unmanageable. There's no unified view of what's running where, no way to preview a session without jumping to it, and no way to quickly resume closed sessions.

## Solution

A single Go binary (`naviclaude`) that runs in a tmux popup window, providing:
- A sidebar listing all active Claude Code panes grouped by tmux session
- A live terminal preview of the selected session
- Full keyboard passthrough to interact with Claude without leaving the manager
- Closed session history with one-key resume via `claude --resume`
- Fuzzy search across all sessions (active + historical)
- Session statistics dashboard

## Architecture

### Approach: Pure Bubble Tea + tmux capture-pane/send-keys

- **Preview**: Periodically capture the tmux pane content via `tmux capture-pane -t <target> -e -p` (preserves ANSI colors). Render in a Bubble Tea viewport.
- **Passthrough**: In passthrough mode, forward keystrokes to the target pane via `tmux send-keys -t <target>`.
- **ANSI rendering pipeline**: `tmux capture-pane -e -p` returns raw text with ANSI escape sequences. Use `charmbracelet/x/ansi` to parse and sanitize these sequences into lipgloss-compatible styled strings for the Bubble Tea viewport. The viewport renders this as pre-styled content.
- **Refresh**: Ticker-based refresh at ~200ms for the active preview pane.
- **Single binary**: Pure Go, no runtime dependencies beyond tmux.

### Why this approach

- Works inside a tmux popup (single pane constraint)
- Simple, well-understood primitives (capture-pane, send-keys)
- The slight refresh delay is acceptable for Claude Code's text-streaming output
- Clean Bubble Tea component architecture

## Layout

### Two-panel: Sidebar (30%) + Preview (70%)

```
┌─────────────────┬──────────────────────────────────────────┐
│ SESSIONS        │ opmed-charts │ main │ ◎ waiting          │
│                 │ CPU 2.1% │ MEM 45MB │ infra:1.2          │
│ ▼ infra      2  │──────────────────────────────────────────│
│   ● opmed-tf 2m │ $ claude                                 │
│  ►◎ charts   5m │                                          │
│                 │ Claude: I've updated the Helm chart...    │
│ ▼ dev        2  │                                          │
│   ● staff   12m │   Would you like me to also update the   │
│   ○ monit    1h │   staging values?                        │
│                 │                                          │
│ ▶ Closed     5  │ ❯ _                                      │
│                 │                                          │
├─────────────────┴──────────────────────────────────────────┤
│ Enter focus │ f jump │ / search │ n new │ K kill │ ? help  │
└────────────────────────────────────────────────────────────┘
```

### Sidebar content per session

- Status icon: `●` active (green), `◎` waiting (amber), `○` idle (gray), `◌` closed (dim)
- Project name (derived from CWD basename)
- Time since last activity (relative)
- Summary line: first user prompt from `~/.claude/history.jsonl` `display` field (truncated)
- Grouped by parent tmux session name, with collapsible groups

### Preview panel

- **Header bar**: project name, git branch, status badge, model indicator (opus/sonnet/haiku — parsed from assistant records in `.jsonl`), tmux target (`session:window.pane`), CPU/memory, uptime
- **Terminal content**: Live captured output from `tmux capture-pane -e -p`
- **Focus indicator**: Blue border when in passthrough mode, dim border in list mode

### Status bar (bottom)

- Shows important keybindings: `Enter focus │ f jump │ / search │ n new │ K kill │ ? help`
- Version number on the right

## Modes

### List Mode (default)

The sidebar is focused. Navigate sessions with `j`/`k`. Preview updates to show the selected session's terminal content (read-only). The preview viewport supports scrolling: `Ctrl+u`/`Ctrl+d` for half-page up/down, `g`/`G` for top/bottom of captured content.

### Passthrough Mode

Entered via `Enter` or `Tab`. The preview panel is focused — all keystrokes are forwarded to the tmux pane via `send-keys`. The sidebar dims. A "● PASSTHROUGH" badge appears. Exit with `Ctrl+]` (matches tmux's nested escape convention). `Tab` also exits passthrough since it's the same key used to enter it, making it a toggle. Note: `Esc` is NOT used to exit passthrough because Claude Code itself uses Escape for its own UI interactions.

### Search Mode

Entered via `/`. A fuzzy search input appears at the top of the sidebar. Filters sessions by: project name, directory path, git branch, session summary text. `Esc` clears the search and returns to list mode.

**Search scope**: Search bypasses the `closed_session_hours` time filter and searches ALL historical sessions from `~/.claude/projects/`. The sidebar's "Closed" group is time-limited for quick browsing, but search is unrestricted. For performance, the session index (see Claude Code File Formats below) is built once on startup and held in memory — with ~800 sessions this is negligible.

## Session Detection

### Active sessions

1. Run `tmux list-panes -a -F '#{session_name}:#{window_index}.#{pane_index} #{pane_current_command} #{pane_pid} #{pane_current_path}'`
2. For each pane, check if `pane_current_command` or the full command line (`ps -p <pid> -o command=`) matches any name in the configurable `process_names` list (default: `["claude"]`). Also walk the process tree recursively from `pane_pid` (using `pgrep -P` at each level) to find matching processes that may be grandchildren (e.g., launched through shell wrappers, nvm shims, symlinks, or mise). This handles aliases (which resolve to `claude` at the process level) and custom binary names like `cc`.
3. This correctly handles multi-pane windows — only Claude panes are shown

### Status detection (from pane content)

Analyze captured pane content by comparing consecutive captures and pattern matching:

- `● active` — content differs from previous capture (captured content changed within the last 2 refresh cycles). Requires storing the previous capture hash per session.
- `◎ waiting` — content is stable AND the last line matches Claude Code's input patterns: the `❯` prompt character, or a `[Y/n]`/`[y/N]` confirmation prompt, or a permission request like `Allow?`.
- `○ idle` — content is stable AND no known input prompt is detected (e.g., a shell prompt `$` is visible, or Claude's output ended without a prompt).

The `preview/status.go` component maintains a `map[tmuxTarget]string` of previous capture content (simple string equality, no hashing needed) to detect changes.

### Waiting Notification

When a session transitions from `active` to `waiting` (content stopped changing AND an input prompt is detected), the session item in the sidebar briefly flashes/highlights with the waiting color (amber). This draws attention to sessions that need user input, helping triage across many simultaneous sessions. The flash lasts ~2 seconds then settles to the normal waiting indicator.

### Closed sessions

1. Scan `~/.claude/projects/**/*.jsonl` files
2. Filter by file modification time for the "recent" sidebar group (default: last 6 hours, configurable)
3. Cross-reference with active sessions to exclude currently running ones
4. Parse session metadata: sessionId (from filename UUID), cwd, gitBranch, timestamps from first/last record
5. Summary text from `~/.claude/history.jsonl` `display` field (joined by sessionId — see File Formats below)

## Claude Code File Formats

### `~/.claude/history.jsonl` (single global file)

One JSON object per line. Each entry represents a session's initial prompt:

```json
{
  "display": "fix the IAM role for eks nodes",     // first user message — used as session summary
  "sessionId": "ed593a46-2ba5-4fbc-802d-13e898993ef6",
  "project": "/Users/tomhalo/git/opmed-tf",         // working directory
  "timestamp": 1773263546788                         // unix millis
}
```

**Join strategy**: Load `history.jsonl` into a `map[sessionId]HistoryEntry` at startup. When displaying a session from `projects/`, look up its UUID in this map to get the `display` text for the summary line.

### `~/.claude/projects/<path-slug>/<sessionId>.jsonl`

Path slug is the working directory with `/` replaced by `-` (e.g., `/Users/tom/git/foo` → `-Users-tom-git-foo`). Each file is named by the session UUID.

Each line is a JSON record with these common fields:

```json
{
  "sessionId": "ed593a46-...",
  "timestamp": "2026-03-09T15:05:25.295Z",   // ISO 8601
  "cwd": "/Users/tomhalo/git/opmed-tf",
  "version": "2.1.71",                        // Claude Code version
  "gitBranch": "main",
  "type": "user|assistant|progress|tool_use|tool_result|last-prompt",
  "slug": "mighty-tickling-sketch",           // memorable session name
  "message": { "content": "..." }             // for user/assistant types
}
```

**Key record types**: `user` (user messages), `assistant` (Claude responses, may embed tool calls), `progress` (streaming output), `system`, `file-history-snapshot`, `queue-operation`. Other types may exist and should be silently skipped by the parser.

**Message structure**: The `message.content` field is polymorphic — sometimes a plain string, sometimes a list of content blocks (for multi-modal or tool-use messages). The parser must handle both: `string` → use directly, `[]interface{}` → extract text blocks.

**Model info**: Available in `assistant`-type records. Model IDs use full names like `claude-opus-4-6` or `claude-sonnet-4-5-20250929`. For display, group by family (opus/sonnet/haiku) by matching the prefix.

### `~/.claude/stats-cache.json` (pre-computed by Claude Code)

Contains aggregated stats including `modelUsage` (token counts per model ID like `claude-opus-4-6`, `claude-sonnet-4-5`). naviClaude uses this as a fast path for model usage percentages rather than scanning all session files.

## Data Model

```go
type Session struct {
    ID           string        // UUID from .jsonl filename or derived
    TmuxSession  string        // parent tmux session name (grouping key)
    TmuxTarget   string        // "session:window.pane" for active sessions
    CWD          string        // working directory
    GitBranch    string        // current branch
    Status       SessionStatus // Active, Idle, Waiting, Closed
    LastActivity time.Time     // last seen activity
    ProjectName  string        // derived from CWD basename
    Summary      string        // first user prompt (from history.jsonl display field)
    CPU          float64       // from ps (active only)
    Memory       float64       // from ps (active only)
    SessionFile  string        // path to .jsonl file (for resume)
}

type SessionStatus int
const (
    StatusActive  SessionStatus = iota
    StatusIdle
    StatusWaiting
    StatusClosed
)
```

## Keybindings

All keybindings are configurable via config file. Defaults:

### List Mode

| Key | Action |
|-----|--------|
| `j` / `k` / `↑` / `↓` | Navigate sessions |
| `Enter` / `Tab` | Focus preview (passthrough mode) |
| `f` | Jump to session's tmux pane (closes popup) |
| `/` | Fuzzy search |
| `n` | New Claude session (prompts for directory via fuzzy path picker, creates pane in current tmux session) |
| `K` | Kill session (inline confirmation prompt at bottom: "Kill opmed-charts? [y/N]") |
| `d` | Detail overlay (shows: full CWD, git branch, session ID, tmux target, .jsonl file path, start time, uptime, message count, slug name) |
| `s` | Stats popup |
| `?` | Help popup (all keybindings) |
| `q` | Quit |

### Passthrough Mode

| Key | Action |
|-----|--------|
| `Tab` / `Ctrl+]` | Return to list mode (Esc NOT used — Claude Code needs it) |
| `Ctrl+f` | Jump to this pane (closes popup) — uses Ctrl modifier to avoid intercepting the letter `f` |
| All other keys | Forwarded to tmux pane via send-keys |

**Key interception in passthrough**: Only `Tab`, `Ctrl+]`, and `Ctrl+f` are intercepted. ALL other keystrokes (including plain letters, Esc, Enter, arrows) are forwarded to the pane. This means you can type freely in Claude Code — no letters are "stolen" by naviClaude.

## Mouse Support

Bubble Tea supports mouse events natively. naviClaude handles:

- **Click session** in sidebar → selects it, preview updates
- **Double-click session** → enters passthrough mode for that session
- **Click preview panel** → enters passthrough mode
- **Scroll wheel in sidebar** → scroll session list
- **Scroll wheel in preview** → scroll captured output (list mode only)

Mouse events work in tmux popups when tmux mouse mode is enabled (`set -g mouse on`).

### Right-click Context Menu

Right-clicking a session in the sidebar opens a floating context menu with the most common actions:

```
┌─────────────────────────┐
│ > Focus         (Enter) │
│ > Jump to pane      (f) │
│ ─────────────────────── │
│   Detail            (d) │
│   Resume   (closed only)│
│ ─────────────────────── │
│   Copy session ID       │
│   Copy project path     │
│ ─────────────────────── │
│ x Kill session      (K) │
└─────────────────────────┘
```

- Menu appears at mouse position
- Navigate with `j`/`k` or mouse hover
- Select with `Enter` or click
- Dismiss with `Esc` or clicking outside
- For closed sessions: "Resume" replaces "Kill", and "Focus"/"Jump" trigger auto-resume
- The keybinding shortcut is shown next to each action for learning

## Closed Session Behavior

When a closed session is selected:

**`Enter` (focus):**
1. naviClaude opens a new tmux pane in the current tmux session
2. Runs `cd <originalCWD> && claude --resume <sessionId>` (cd first — Claude Code has no `--cwd` flag)
3. The session becomes active and moves to its tmux session group
4. Preview shows the live pane and naviClaude enters **passthrough mode** (consistent with Enter on active sessions)

**`f` (jump):**
1. Same resume steps (1-3 above)
2. naviClaude exits (closes the popup) and the tmux client switches to the new pane

Both are seamless — no confirmation dialog, no manual steps.

### Fork & Resume

In addition to regular resume, the context menu offers "Fork & Resume" which runs `claude --resume <sessionId> --fork-session`. This creates a new session ID, branching the conversation without overwriting the original. Useful for exploring an alternative approach from a checkpoint.

## Statistics Popup

Triggered by `s` key. Shows an overlay with:

### Key metrics
- Total sessions (count of `.jsonl` files in `~/.claude/projects/`)
- Total messages (from `stats-cache.json` `totalMessages` field — pre-computed by Claude Code)
- Currently active count (from session detection)
- Average sessions per day (total sessions / days since `stats-cache.json` `firstSessionDate`)

### Charts/breakdowns
- Top projects by session count (bar chart, computed from `.jsonl` file counts per project directory)
- Weekly activity (7-day bar chart). Note: `stats-cache.json`'s `dailyActivity` may be stale — if the `lastComputedDate` is older than 24h, compute recent daily activity by scanning `.jsonl` file modification times instead.
- Model usage breakdown (grouped by family: opus/sonnet/haiku, from `stats-cache.json` `modelUsage` field which uses full model IDs like `claude-opus-4-6`)
- Longest session (duration + message count)
- Peak productivity hour

### Computation
- **Lazy**: Stats are computed on first open, with a loading spinner/progress indicator
- **Cached**: Results stored in `~/.config/naviclaude/stats-cache.json`
- **Invalidated**: When file count in `~/.claude/projects/` changes or cache is older than 1 hour
- **Filterable**: Toggle between "All time" / "This week" / "Today"

## Configuration

File: `~/.config/naviclaude/config.yaml`

```yaml
# Keybindings (all configurable)
keys:
  focus: "enter"
  jump: "f"
  search: "/"
  new_session: "n"
  kill_session: "K"
  detail: "d"
  stats: "s"
  help: "?"
  quit: "q"

# Display
sidebar_width: 30          # percentage
refresh_interval: 200ms    # preview refresh rate
closed_session_hours: 6    # how far back to show in "Closed" group

# Tmux popup (for the keybinding in tmux.conf)
popup_width: 85            # percentage
popup_height: 85           # percentage

# Session resume
resume_in_current_session: true  # resume in current tmux session vs original

# Process detection
process_names: ["claude"]        # add aliases/symlinks like "cc" if needed
```

## Implementation Phases

### Phase 1 (MVP) — Core session management
- Session detection (active + closed)
- Sidebar with tmux session grouping
- Live preview via capture-pane
- Passthrough mode via send-keys
- Jump to pane (`f`)
- Resume closed sessions (`Enter`/`f`)
- Fuzzy search (`/`)
- Status bar with keybinding hints
- Help popup (`?`)
- Responsive layout
- Hardcoded keybindings (configurable in Phase 2)

### Phase 2 — Polish
- Configurable keybindings (config.yaml)
- Statistics popup (`s`)
- Detail overlay (`d`)
- New session creation (`n`)
- Kill session (`K`)
- CPU/memory monitoring (via `ps -p <pid> -o %cpu,%mem`, refreshed every 5s on a separate slower ticker)

### Phase 3 — CI/CD & Distribution
- GitHub Actions workflow for build (multi-platform: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)
- Semantic version bumping (via git tags)
- Automated GitHub releases with pre-built binaries (using GoReleaser)
- Homebrew tap formula for `brew install naviclaude`

This phasing lets us ship a usable tool quickly, then layer on features.

## Error Handling

- **Pane disappears between list and capture**: Gracefully remove from the session list on next refresh. If the selected session disappears, auto-select the nearest remaining session.
- **tmux not running**: Show error message "tmux is not running. Please start a tmux session first." and exit.
- **send-keys fails** (pane killed during passthrough): Exit passthrough mode, remove session from list, show brief notification.
- **No sessions found**: Show an empty state with a hint: "No Claude Code sessions found. Press `n` to create one, or start Claude Code in a tmux pane."
- **Malformed .jsonl files**: Skip unparseable lines/files silently. Log to stderr if `--debug` flag is set.

## tmux Integration

Add to `~/.tmux.conf`:

```bash
bind-key g display-popup -E -w 85% -h 85% -x C -y C "naviclaude"
```

Then `prefix + g` opens naviClaude as a centered floating overlay from anywhere.

## Responsive / Resizable

- The TUI must handle terminal resize events (Bubble Tea's `tea.WindowSizeMsg`)
- Sidebar width is a percentage, so it scales with terminal width
- Below a minimum width threshold, switch to a single-panel mode (list only, Enter to see preview fullscreen)
- The preview viewport adjusts to available height minus the header and status bar

## Project Structure

```
naviClaude/
├── cmd/naviclaude/main.go          # Entry point, CLI flags
├── internal/
│   ├── app/
│   │   ├── app.go                  # Main Bubble Tea model, Update, View
│   │   ├── keys.go                 # Keybinding definitions (reads config)
│   │   └── modes.go                # Mode management (list, passthrough, search)
│   ├── session/
│   │   ├── session.go              # Session data model
│   │   ├── detector.go             # Discover active sessions from tmux
│   │   ├── history.go              # Scan .jsonl files for closed sessions
│   │   └── manager.go              # CRUD: resume, kill, create
│   ├── tmux/
│   │   ├── client.go               # tmux command wrapper
│   │   └── parser.go               # Parse tmux output formats
│   ├── preview/
│   │   ├── capture.go              # capture-pane with ANSI preservation
│   │   ├── passthrough.go          # send-keys forwarding
│   │   └── status.go               # Detect active/idle/waiting from content
│   ├── stats/
│   │   ├── collector.go            # Scan .jsonl files, compute stats
│   │   └── cache.go                # Stats caching
│   ├── ui/
│   │   ├── sidebar.go              # Session list with tree grouping
│   │   ├── preview.go              # Terminal preview viewport
│   │   ├── statusbar.go            # Bottom keybinding hints
│   │   ├── search.go               # Fuzzy search input + filtering
│   │   ├── stats.go                # Stats overlay
│   │   ├── detail.go               # Session detail overlay
│   │   └── help.go                 # Help overlay
│   ├── config/
│   │   └── config.go               # YAML config loading
│   └── styles/
│       └── styles.go               # Lipgloss style definitions
├── go.mod
├── go.sum
├── Makefile                         # build, install, clean
└── README.md
```

## Dependencies

- [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [charmbracelet/bubbles](https://github.com/charmbracelet/bubbles) — viewport, textinput components
- [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) — styling
- [charmbracelet/x/ansi](https://github.com/charmbracelet/x) — ANSI sequence parsing for captured pane content
- [sahilm/fuzzy](https://github.com/sahilm/fuzzy) — fuzzy matching for search
- [gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3) — config file parsing
- tmux >= 3.2 (for popup support)

## Verification

### Manual testing
1. Build: `go build -o naviclaude ./cmd/naviclaude`
2. Start a few Claude Code sessions in different tmux sessions
3. Run `naviclaude` — verify sessions appear grouped by tmux session
4. Navigate with j/k — verify preview updates
5. Press Enter — verify passthrough mode (type, see response in preview)
6. Press Tab or Ctrl+] — verify return to list mode (NOT Esc)
7. Press f — verify jump to tmux pane
8. Close a session, reopen naviclaude — verify it appears in "Closed" group
9. Select closed session + Enter — verify auto-resume with `--resume`
10. Press / — verify fuzzy search across active + closed sessions
11. Press s — verify stats popup loads (with spinner on first load)
12. Test in tmux popup: `tmux display-popup -E -w 85% -h 85% naviclaude`
13. Resize the popup — verify layout adapts

### Edge cases
- No active Claude sessions (show empty state with hint)
- Single pane in a window vs multi-pane windows
- Very long session summaries (truncation)
- Very many sessions (scrollable sidebar)
- No `.jsonl` files yet (new install)
- tmux not running (error message + exit)
