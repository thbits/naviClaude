---
name: naviclaude
description: Coordinate other Claude Code sessions running in tmux with the naviclaude CLI — list them and their status, wait until one finishes or needs input, and send one a prompt (optionally waiting for the reply). Use when you need to dispatch work to, check on, or wait for another Claude session, or when the user mentions their other sessions/panes/agents in tmux.
---

# Driving other Claude sessions with naviclaude

`naviclaude` sees every Claude Code session running in a tmux pane (the TUI need not be running).

## List

```sh
naviclaude list           # table: PANE TARGET NAME STATUS PROJECT BRANCH MODEL LAST ID; * = your own pane
naviclaude list --json    # array of {id, name, status, reason, background, tmux_session, target,
                          #   pane_id, cwd, project, branch, model, last_activity, pid, self, subprocess}
```

Status is one of:
- `working`: mid-turn, or background agents/shells/monitors still running (`background` says which).
- `waiting`: blocked on the user. `reason` is e.g. `needs approval: Bash`, `question`, `plan approval`.
- `done`: the turn finished and nobody has looked yet. Needs `naviclaude hooks install`; without hooks such sessions show `idle`.
- `idle`: at the prompt.

## Target a session

Any of: session id or a unique prefix of at least 6 characters (`3cb8566e`), pane id (`%185`), tmux target (`work:2.0`), or a unique name. Ambiguous targets fail and list the candidates. Prefer pane ids in scripts: they stay stable across `/clear`.

## Wait

```sh
naviclaude wait %185 --until idle            # turn ended (idle or done), held for 2 polls
naviclaude wait %185 --until any-attention   # waiting or done
naviclaude wait %185 --until waiting --timeout 30m --interval 2s
```

The `--until` values are `waiting`, `working`, `idle`, `done` and `any-attention`. The default timeout is `--timeout 10m`, and `0` means never. Pass `--json` to get the final record.

It prints the final status, e.g. `waiting (needs approval: Edit)`. Exit codes: 0 when the state is reached, 1 on timeout, 2 on bad arguments or when the session is gone.

## Send

```sh
naviclaude send %185 "run the tests and fix failures"          # paste + Enter, returns at once
naviclaude send %185 --wait --timeout 20m -- "review the diff"  # block until idle/done or waiting
git diff | naviclaude send %185 - --wait                        # prompt from stdin
```

Pass the prompt as one quoted argument or after `--`. Flags are parsed anywhere, so an unquoted `--force` inside the text would be taken as a flag.

`send` refuses, unless you pass `--force`, when the target:
- is `waiting`, because the text would answer its permission prompt or question;
- is your own session;
- is embedded in another app.

If a prompt opens between the paste and Enter, `send` leaves the text in the input, does not press Enter and exits 2.

`--wait` first confirms the turn started, then waits for it to end. Exit codes are the same as `wait`, plus 3 when the prompt did not start a turn within 5s; check the pane before resending.

## Dispatch and collect

```sh
pane=$(naviclaude list --json | jq -r '.[] | select(.name=="api-tests") | .pane_id')
naviclaude send "$pane" --wait --timeout 30m -- "Fix the failing test in pkg/api and summarise the fix"
case $? in
  0) echo "finished or needs input: $(naviclaude list --json | jq -r --arg p "$pane" '.[]|select(.pane_id==$p)|.status')" ;;
  1) echo "still working after 30m" ;;
  3) echo "prompt did not start; check the pane" ;;
  *) echo "send failed" ;;
esac
tmux capture-pane -p -t "$pane" -S -60   # read its answer
```

If a dispatched session ends up `waiting`, tell the user which pane needs them rather than approving on their behalf.
