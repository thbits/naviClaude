# naviClaude

TUI for managing Claude Code sessions across tmux sessions.

A "bring your own tmux" alternative to tools like [cmux](https://github.com/manaflow-ai/cmux) --
layers on top of your existing tmux setup instead of replacing it.

![naviClaude](assets/naviclaude.png)

## Features

- **Session sidebar** -- lists active and recently closed Claude Code sessions, grouped by project
- **Live preview** -- real-time tmux pane capture of the selected session
- **Activity sparklines** -- per-session message activity visualization on the selected item
- **Context usage bar** -- shows current token usage vs context window limit
- **Breathing status dots** -- active sessions pulse green, waiting sessions pulse amber, a blue `◉` marks a session that finished while you were away
- **Attention alerts** -- a sound and a colored bell / check after the tmux window name when Claude needs you or finishes, even while naviClaude is closed (see [Notifications](#notifications))
- **Passthrough mode** -- type directly into a session without leaving the TUI
- **Changed-files sidebar** -- a right-hand panel listing the files the selected session edited, with per-file `+/-` counts (live `git diff` while uncommitted, a persistent transcript estimate afterwards); open any file in your editor
- **Fuzzy search** -- filter sessions by name
- **Session management** -- create new sessions, kill existing ones, and resume closed sessions into any tmux session (or a brand-new one) via a target picker
- **Detail popup** -- view session metadata (model, project, cost, token counts)
- **Rich preview header** -- status badge, uptime, message count, CPU/memory
- **Usage statistics** -- per-project breakdown with bar charts
- **10 built-in color themes** with a live theme picker
- **Configurable key bindings** and layout options
- **Auto-collapse** -- idle project groups collapse automatically

## Requirements

- [tmux](https://github.com/tmux/tmux/wiki) -- naviClaude must be run inside a tmux session (`brew install tmux`)
- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) (`claude`)

## Installation

### Homebrew

```
brew install thbits/tap/naviclaude
```

### Manual

```
git clone https://github.com/thbits/naviClaude.git
cd naviClaude
make install
```

This installs the binary to `~/.local/bin/naviclaude`. Set a custom prefix with `PREFIX=/usr/local make install`.

### Run as a tmux popup

Bind it to a key in `~/.config/tmux/tmux.conf` to open a centered popup with `<prefix> g`:

```tmux
bind g display-popup -E -w 85% -h 85% -x C -y C "naviclaude"
```

## Usage

Launch naviClaude from any terminal inside tmux:

```
naviclaude
```

### Key bindings

| Key | Action |
|---|---|
| `j` / `k` | Navigate sessions |
| `Tab` / `Shift+Tab` | Cycle focus across the visible panes (session list -> preview passthrough -> changed files) |
| `Ctrl+]` | Exit passthrough straight to the session list |
| `Ctrl+U` / `Ctrl+D` | Scroll preview |
| `/` | Search |
| `Enter` | Focus an active session (passthrough mode); on a closed session, open the resume picker |
| `f` | Jump to pane. On a closed session, fast-resume into the current tmux session |
| `Ctrl+F` | Jump to pane (from passthrough) |
| `n` | New session (same tmux session) |
| `N` | New tmux session (prompts for name) |
| `r` | Rename session |
| `K` | Kill session |
| `F` | Toggle the changed-files sidebar and focus it (`Esc` drops focus but keeps it open) |
| `e` / `Enter` | In the changed-files sidebar, open the selected file in your editor |
| `d` | Detail popup |
| `s` | Statistics popup |
| `T` | Theme picker |
| `?` | Help overlay |
| `q` | Quit |

All key bindings except navigation and passthrough exit keys are configurable.

## Configuration

Config file location: `~/.config/naviclaude/config.yaml`

naviClaude creates this file automatically when you save a theme. All fields are optional -- missing fields use defaults.

```yaml
keys:
  quit: "q"
  search: "/"
  focus: "enter"
  jump: "f"
  new_session: "n"
  new_tmux_session: "N"
  kill_session: "K"
  rename_session: "r"
  detail: "d"
  stats: "s"
  toggle_changed: "F"
  help: "?"

sidebar_width: 30
refresh_interval: "200ms"
closed_session_hours: 8
group_sort_order: "name"
session_sort_order: "name"
popup_width: 85
popup_height: 85
resume_in_current_session: true
process_names: ["claude"]
collapse_after_hours: 8
active_window_secs: 5
cpu_active_threshold: 5
theme: "tokyo-night"
claude_command: "claude"
check_for_updates: true
focus_last_session: true
remember_group_state: true
# editor: "cursor"   # falls back to $EDITOR, then vi
# new_session_dir: "~/projects"
```

| Option | Default | Description |
|---|---|---|
| `sidebar_width` | `30` | Width of the session list panel |
| `refresh_interval` | `200ms` | How often to refresh session data |
| `closed_session_hours` | `8` | Show closed sessions from the last N hours |
| `group_sort_order` | `name` | Sort tmux session groups: `name` (alphabetical) or `activity` |
| `session_sort_order` | `name` | Sort sessions within groups: `name` (alphabetical) or `activity` |
| `popup_width` | `85` | Popup width (percentage of terminal) |
| `popup_height` | `85` | Popup height (percentage of terminal) |
| `resume_in_current_session` | `true` | Resume sessions in the current tmux pane |
| `process_names` | `["claude"]` | Process names to detect as Claude sessions |
| `collapse_after_hours` | `8` | Auto-collapse groups idle longer than N hours (0 to disable) |
| `active_window_secs` | `5` | Seconds after last transcript write to keep a session "working" (fallback signal) |
| `cpu_active_threshold` | `5` | Process-subtree CPU% above which a session counts as "working" |
| `theme` | `tokyo-night` | Color theme name |
| `claude_command` | `claude` | Command to start Claude (sent via send-keys, supports aliases) |
| `check_for_updates` | `true` | Check GitHub on startup for a newer release and show "update available" in the status bar |
| `focus_last_session` | `true` | On startup, re-focus the session you last had selected; falls back to the first session if it no longer exists |
| `remember_group_state` | `true` | On startup, restore the open/closed state of groups you manually toggled |
| `editor` | `$EDITOR`, else `vi` | Editor for opening changed files from the changed-files sidebar; may include flags. GUI editors (`cursor`, `code`, `zed`, ...) open in their own window; terminal editors (`vim`, `nvim`, `nano`, ...) take over the TUI and return on exit |
| `new_session_dir` | `~` | Working directory for new tmux sessions created with `N` |

## Notifications

naviClaude can ring a bell and mark the tmux window when a Claude session needs you. It works through Claude Code hooks, so it fires whether or not the dashboard is open:

```sh
naviclaude hooks install    # adds async hooks to ~/.claude/settings.json (backup in ~/Library/Caches/naviclaude)
naviclaude hooks status
naviclaude hooks uninstall
```

Restart running Claude sessions afterwards; hooks are read at session start. The hooks call this binary by absolute path, so re-run `install` if you move it.

| Event | What happens |
|---|---|
| Permission prompt, question (`AskUserQuestion`), plan approval, MCP input | **request** sound, a yellow bell `` is appended to the window name -- the marker clears as soon as Claude stops waiting (answered, Esc, ...) or you switch to the pane |
| Turn finished in a pane you are not looking at | **done** sound, a green check `` is appended until you visit it or send the next prompt |
| Turn finished while you are looking at it | nothing (unless `done_sound_when_focused`) |

Hooks are only triggers: before ringing, naviClaude confirms the state with Claude's own status file (`~/.claude/sessions/<pid>.json`), so prompts that auto mode approves instantly stay silent and the marker never sticks. Sounds respect a global cooldown so several sessions finishing together ring once. Outside tmux the hooks do nothing.

The default sounds are herdr's (Apache-2.0). Preview them with `naviclaude sound request` / `naviclaude sound done`. To customize:

```yaml
notifications:
  enabled: true
  request_sound: ""                # "" = built-in, a file path (mp3/aiff/wav), or "none"
  done_sound: "/System/Library/Sounds/Glass.aiff"
  done_sound_when_focused: false
  volume: 1                        # 0-1
  cooldown: "2s"
  window_marker: true
  waiting_marker: "#[fg=yellow,bold]#[nobold]"   # nf-fa-bell; any text, tmux #[...] styles allowed
  done_marker: "#[fg=green,bold]#[nobold]"      # nf-fa-check
```

The markers need a [Nerd Font](https://www.nerdfonts.com/); swap in plain text such as `!` / `+` if you do not use one. The marker is a suffix, so your `window-status-format` colors after `#W` stay intact; the original name and its `automatic-rename` setting are restored when it clears. If you rename the window while it is marked, your new name is kept. To debug, `touch ~/Library/Caches/naviclaude/hook.log` and watch it.

## Themes

Available built-in themes:

- `catppuccin-latte`
- `catppuccin-mocha`
- `dracula`
- `gruvbox`
- `kanagawa`
- `nord`
- `one-dark`
- `rose-pine`
- `solarized-dark`
- `tokyo-night` (default)

Press `T` to open the theme picker with live color swatches. Selected themes are saved to the config file.

## License

MIT
