package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Exit codes shared by wait and send.
const (
	exitMatched = 0
	exitTimeout = 1
	exitUsage   = 2 // bad arguments, unknown/ambiguous target, or the session went away
	exitNoStart = 3 // send --wait: the prompt never started a turn
)

// clock is the time surface the wait loop needs; tests use a fake.
type clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time        { return time.Now() }
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// waitCond is a target state: match picks the statuses, settle those that must
// hold for settlePolls consecutive polls before counting.
type waitCond struct {
	name   string
	match  func(a agentInfo) bool
	settle func(a agentInfo) bool
}

// settlePolls debounces turn-ended states so a gap between tool calls isn't taken for idle.
const settlePolls = 2

func isEnded(a agentInfo) bool { return a.Status == stIdle || a.Status == stDone }

// untilConds are the states wait --until accepts; idle includes done (an idle
// pane the Stop hook marked unseen).
var untilConds = map[string]waitCond{
	"waiting":       {name: "waiting", match: func(a agentInfo) bool { return a.Status == stWaiting }, settle: never},
	"working":       {name: "working", match: func(a agentInfo) bool { return a.Status == stWorking }, settle: never},
	"idle":          {name: "idle", match: isEnded, settle: isEnded},
	"done":          {name: "done", match: func(a agentInfo) bool { return a.Status == stDone }, settle: isEnded},
	"any-attention": {name: "any-attention", match: func(a agentInfo) bool { return a.Status == stWaiting || a.Status == stDone }, settle: isEnded},
}

// turnOver is send --wait's final state: the turn ended or it needs the user.
var turnOver = waitCond{
	name:   "idle or waiting",
	match:  func(a agentInfo) bool { return a.Status == stWaiting || isEnded(a) },
	settle: isEnded,
}

func never(agentInfo) bool { return false }

// maxSourceErrors is how many listings in a row may fail before a wait gives up.
const maxSourceErrors = 3

// waitOutcome is how a wait ended.
type waitOutcome int

const (
	waitMatched waitOutcome = iota
	waitTimedOut
	waitGone
)

// waiter polls src for one pane until a condition holds.
type waiter struct {
	src      agentSource
	clk      clock
	interval time.Duration
}

// waitFor polls until the session in pane (falling back to id when it has no
// pane) meets c; timeout <= 0 waits forever. It returns the last state seen.
func (w waiter) waitFor(ref agentInfo, c waitCond, timeout time.Duration) (agentInfo, waitOutcome, error) {
	deadline := w.clk.Now().Add(timeout)
	last := ref
	streak, errs := 0, 0
	for {
		list, err := w.src()
		if err != nil {
			// A busy tmux or ps can fail one listing; only a run of failures means gone.
			if errs++; errs >= maxSourceErrors {
				return last, waitGone, err
			}
			if timeout > 0 && !w.clk.Now().Before(deadline) {
				return last, waitTimedOut, nil
			}
			w.clk.Sleep(w.interval)
			continue
		}
		errs = 0
		cur, ok := findSame(list, ref)
		if !ok {
			return last, waitGone, nil
		}
		last = cur
		if c.match(cur) {
			streak++
			if !c.settle(cur) || streak >= settlePolls {
				return cur, waitMatched, nil
			}
		} else {
			streak = 0
		}
		if timeout > 0 && !w.clk.Now().Before(deadline) {
			return cur, waitTimedOut, nil
		}
		w.clk.Sleep(w.interval)
	}
}

// findSame locates ref's pane in list; the session ID changes on /clear, so the
// pane is the identity that survives.
func findSame(list []agentInfo, ref agentInfo) (agentInfo, bool) {
	for _, a := range list {
		if ref.PaneID != "" && a.PaneID == ref.PaneID {
			return a, true
		}
		if ref.PaneID == "" && ref.ID != "" && a.ID == ref.ID {
			return a, true
		}
	}
	return agentInfo{}, false
}

// waitOpts are the flags wait and send --wait share.
type waitOpts struct {
	timeout  time.Duration
	interval time.Duration
	json     bool
}

func (o *waitOpts) register(fs *flag.FlagSet, defTimeout time.Duration) {
	fs.DurationVar(&o.timeout, "timeout", defTimeout, "give up after this long (0 = never)")
	fs.DurationVar(&o.interval, "interval", time.Second, "poll interval")
	fs.BoolVar(&o.json, "json", false, "print the final session state as JSON")
}

func (o waitOpts) validate() error {
	if o.interval < 50*time.Millisecond {
		return errors.New("--interval must be at least 50ms")
	}
	if o.timeout < 0 {
		return errors.New("--timeout must not be negative")
	}
	return nil
}

// runWait blocks until a session reaches a state.
func runWait(args []string) int {
	return waitMain(args, func() agentSource { return newLiveSource(false).Agents }, realClock{})
}

// waitMain is runWait with its session source and clock injected.
func waitMain(args []string, newSrc func() agentSource, clk clock) int {
	fs := flag.NewFlagSet("naviclaude wait", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: naviclaude wait <target> --until waiting|working|idle|done|any-attention [--timeout 10m] [--interval 1s] [--json]")
		fmt.Fprintln(os.Stderr, "\nBlock until a session reaches a state. <target> is a session id (or unique prefix),")
		fmt.Fprintln(os.Stderr, "pane id (%12), tmux target (sess:1.0) or unique name. idle also matches done.")
		fmt.Fprintln(os.Stderr, "Exit: 0 reached, 1 timed out, 2 bad arguments or session gone.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	until := fs.String("until", "", "state to wait for: waiting, working, idle, done or any-attention")
	var o waitOpts
	o.register(fs, 10*time.Minute)
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return exitUsage
	}
	cond, ok := untilConds[*until]
	if len(pos) != 1 || !ok {
		if *until != "" && !ok {
			fmt.Fprintf(os.Stderr, "naviclaude wait: unknown --until %q\n", *until)
		}
		fs.Usage()
		return exitUsage
	}
	if err := o.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude wait:", err)
		return exitUsage
	}
	src := newSrc()
	list, err := src()
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude wait:", err)
		return exitUsage
	}
	ref, err := resolveTarget(list, pos[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude wait:", err)
		return exitUsage
	}
	w := waiter{src: src, clk: clk, interval: o.interval}
	final, outcome, err := w.waitFor(ref, cond, o.timeout)
	return reportWait("wait", final, outcome, err, cond, o)
}

// reportWait prints the final state and maps the outcome to an exit code.
func reportWait(cmd string, final agentInfo, outcome waitOutcome, err error, c waitCond, o waitOpts) int {
	switch outcome {
	case waitGone:
		if err != nil {
			fmt.Fprintf(os.Stderr, "naviclaude %s: %v\n", cmd, err)
		} else {
			fmt.Fprintf(os.Stderr, "naviclaude %s: session %s (%s) is gone\n", cmd, final.Name, final.PaneID)
		}
		return exitUsage
	case waitTimedOut:
		msg := fmt.Sprintf("naviclaude %s: timed out after %s waiting for %s; %s is %s", cmd, o.timeout, c.name, final.Name, final.statusLabel())
		if final.Background != "" {
			msg += " (background work counts as working)"
		}
		fmt.Fprintln(os.Stderr, msg)
		printAgent(final, o.json)
		return exitTimeout
	}
	printAgent(final, o.json)
	return exitMatched
}

// printAgent prints the status label, or the full record as JSON.
func printAgent(a agentInfo, asJSON bool) {
	if asJSON {
		b, _ := json.MarshalIndent(a, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Println(strings.TrimSpace(a.statusLabel()))
}
