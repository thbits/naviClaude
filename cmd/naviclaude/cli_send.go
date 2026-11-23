package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/tmux"
)

// paneTyper is the tmux surface send needs; *tmux.Client satisfies it.
type paneTyper interface {
	PasteText(target, text string) error
	SendKeys(target, keys string) error
}

// turnStartWindow bounds how long send --wait looks for the prompt to start a
// turn, so a stale idle before Claude reacts isn't taken for completion.
const turnStartWindow = 5 * time.Second

// submitDelay lets the TUI finish ingesting a bracketed paste before Enter.
const submitDelay = 150 * time.Millisecond

// sendDeps are send's injectable side effects.
type sendDeps struct {
	src   agentSource
	typer paneTyper
	clk   clock
	stdin io.Reader
	// waitingNow re-reads whether ref is waiting right before Enter; nil polls src.
	waitingNow func(ref agentInfo) (reason string, waiting bool, err error)
}

// runSend types a prompt into a session and submits it.
func runSend(args []string) int {
	return sendMain(args, func() sendDeps {
		src := newLiveSource(false).Agents
		return sendDeps{src: src, typer: tmux.New(), clk: realClock{}, stdin: os.Stdin, waitingNow: nativeWaiting(src)}
	})
}

// nativeWaiting reads Claude's status file, the cheapest fresh read, and falls
// back to a full listing when the session has none.
func nativeWaiting(src agentSource) func(agentInfo) (string, bool, error) {
	slow := listedWaiting(src)
	return func(ref agentInfo) (string, bool, error) {
		if ref.PID > 0 {
			if st, ok := claude.ReadNativeState(ref.PID, ref.ID); ok {
				return st.WaitingFor, st.Status == "waiting", nil
			}
		}
		return slow(ref)
	}
}

// listedWaiting re-lists sessions and reports ref's waiting state; a vanished session is an error.
func listedWaiting(src agentSource) func(agentInfo) (string, bool, error) {
	return func(ref agentInfo) (string, bool, error) {
		list, err := src()
		if err != nil {
			return "", false, err
		}
		cur, ok := findSame(list, ref)
		if !ok {
			return "", false, fmt.Errorf("session %s (%s) is gone", ref.Name, ref.PaneID)
		}
		return cur.Reason, cur.Status == stWaiting, nil
	}
}

// sendMain is runSend with its dependencies injected.
func sendMain(args []string, deps func() sendDeps) int {
	fs := flag.NewFlagSet("naviclaude send", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: naviclaude send <target> <text...> [--wait] [--timeout 10m] [--force] [--json]")
		fmt.Fprintln(os.Stderr, "       naviclaude send <target> - [--wait]     # read the prompt from stdin")
		fmt.Fprintln(os.Stderr, "\nType a prompt into a session's pane and submit it. Pass the prompt as one quoted")
		fmt.Fprintln(os.Stderr, "argument or after --: flags are parsed anywhere among the words.")
		fmt.Fprintln(os.Stderr, "Refuses a session waiting on a prompt/question, your own session")
		fmt.Fprintln(os.Stderr, "and one embedded in another app unless --force.")
		fmt.Fprintln(os.Stderr, "With --wait, block until the turn ends (idle/done) or the session needs you (waiting).")
		fmt.Fprintln(os.Stderr, "Exit: 0 sent (and finished), 1 timed out, 2 bad arguments, refused or session gone,")
		fmt.Fprintln(os.Stderr, "      3 (--wait) the prompt did not start a turn within "+turnStartWindow.String()+".")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	wait := fs.Bool("wait", false, "wait until the turn ends or the session needs input")
	force := fs.Bool("force", false, "send even if the session is waiting on a prompt, is your own, or is embedded in another app")
	var o waitOpts
	o.register(fs, 10*time.Minute)
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return exitUsage
	}
	if len(pos) < 2 {
		fs.Usage()
		return exitUsage
	}
	if err := o.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude send:", err)
		return exitUsage
	}
	d := deps()
	text := strings.Join(pos[1:], " ")
	if len(pos) == 2 && pos[1] == "-" {
		b, err := io.ReadAll(d.stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "naviclaude send: read stdin:", err)
			return exitUsage
		}
		text = string(b)
	}
	// A trailing newline would submit early or leave an empty line in the prompt.
	text = strings.TrimRight(text, "\r\n")
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(os.Stderr, "naviclaude send: empty prompt")
		return exitUsage
	}

	list, err := d.src()
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude send:", err)
		return exitUsage
	}
	ref, err := resolveTarget(list, pos[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude send:", err)
		return exitUsage
	}
	if why := refuseSend(ref); why != "" && !*force {
		fmt.Fprintf(os.Stderr, "naviclaude send: refusing: %s %s (use --force to send anyway)\n", ref.Name, why)
		return exitUsage
	}
	pane := ref.PaneID
	if pane == "" {
		pane = ref.Target
	}
	if err := d.typer.PasteText(pane, text); err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude send:", err)
		return exitUsage
	}
	d.clk.Sleep(submitDelay)
	if !*force {
		check := d.waitingNow
		if check == nil {
			check = listedWaiting(d.src)
		}
		// A dialog that opened since the first check would be approved by Enter.
		reason, waiting, err := check(ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "naviclaude send: not submitting: re-check %s: %v; the text is pasted but not sent\n", ref.Name, err)
			return exitUsage
		}
		if waiting {
			fmt.Fprintf(os.Stderr, "naviclaude send: not submitting: %s started waiting (%s) after the paste; the text is left in its input\n", ref.Name, reason)
			return exitUsage
		}
	}
	if err := d.typer.SendKeys(pane, "Enter"); err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude send:", err)
		return exitUsage
	}
	if !*wait {
		return exitMatched
	}

	w := waiter{src: d.src, clk: d.clk, interval: min(o.interval, time.Second)}
	start := d.clk.Now()
	started := waitCond{
		name: "turn start",
		match: func(a agentInfo) bool {
			return a.Status == stWorking || a.Status == stWaiting || a.stamp != ref.stamp
		},
		settle: never,
	}
	window := turnStartWindow
	if o.timeout > 0 && o.timeout < window {
		window = o.timeout
	}
	cur, outcome, err := w.waitFor(ref, started, window)
	if outcome == waitGone {
		return reportWait("send", cur, outcome, err, turnOver, o)
	}
	if outcome == waitTimedOut {
		if window < turnStartWindow {
			return reportWait("send", cur, waitTimedOut, nil, started, o)
		}
		fmt.Fprintf(os.Stderr, "naviclaude send: prompt did not start: %s showed no activity within %s\n", ref.Name, window)
		printAgent(cur, o.json)
		return exitNoStart
	}
	rest := time.Duration(0)
	if o.timeout > 0 {
		if rest = o.timeout - d.clk.Now().Sub(start); rest <= 0 {
			return reportWait("send", cur, waitTimedOut, nil, turnOver, o)
		}
	}
	w.interval = o.interval
	final, outcome, err := w.waitFor(cur, turnOver, rest)
	return reportWait("send", final, outcome, err, turnOver, o)
}

// refuseSend says why typing into a would be unsafe, or "" when it is fine.
func refuseSend(a agentInfo) string {
	switch {
	case a.Status == stWaiting:
		return "is waiting (" + a.Reason + "); typing would answer the prompt"
	case a.Self:
		return "is your own session"
	case a.Subprocess:
		return "runs inside another app; its pane belongs to that app"
	}
	return ""
}
