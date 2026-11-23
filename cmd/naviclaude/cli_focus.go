package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/notify"
	"github.com/thbits/naviClaude/internal/tmux"
)

// focuser is the tmux surface runFocus needs; *tmux.Client satisfies it.
type focuser interface {
	launcher
	ListClients() ([]tmux.ClientInfo, error)
	ListInstances() ([]tmux.Instance, error)
	RequestSelect(pane, target string) error
	Reveal(client, pane string) error
}

type focusRequest struct {
	target string // pane id or session id for the dashboard to select
	client string // "" picks the most recently active client
	open   openRequest
	alive  func(pid int) bool
}

// runFocus takes the user to naviClaude with a session selected: the running
// dashboard if there is one, else a fresh one. It is a banner's click command.
func runFocus(args []string) int {
	fs := flag.NewFlagSet("naviclaude focus", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: naviclaude focus <pane-or-session-id> [-c client] [-S socket]")
		fmt.Fprintln(os.Stderr, "\nShow naviClaude with that session selected: the running dashboard if there is one, else a new one.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	client := fs.String("c", "", "target tmux client (default: the most recently active one)")
	sock := fs.String("S", "", "tmux server socket (default: from $TMUX)")
	// Flags may follow the target, as in the banner's command line.
	var target string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		if target != "" {
			fmt.Fprintln(os.Stderr, "naviclaude focus: one target only")
			return 2
		}
		target, rest = fs.Arg(0), fs.Args()[1:]
	}
	if target == "" {
		fs.Usage()
		return 2
	}
	useSocket(*sock)
	exe, err := notify.SelfExecutable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude: resolve own path:", err)
		return 1
	}
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude: config:", err)
	}
	req := focusRequest{
		target: target,
		client: *client,
		alive:  pidAlive,
		open: openRequest{
			command:   tmux.ShellQuote(exe) + " --select " + tmux.ShellQuote(target),
			widthPct:  cfg.PopupWidth,
			heightPct: cfg.PopupHeight,
		},
	}
	if err := focusSession(tmux.New(), req); err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude:", err)
		return 1
	}
	return 0
}

// useSocket points every tmux call at sock, as launchd's bare environment has no
// $TMUX, and makes sure tmux itself is on PATH.
func useSocket(sock string) {
	if sock != "" {
		os.Setenv("TMUX", sock+",0,0")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		os.Setenv("PATH", os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin:/opt/local/bin")
	}
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// focusSession reveals a running dashboard on the client, or opens one there.
func focusSession(f focuser, r focusRequest) error {
	clients, err := f.ListClients()
	if err != nil {
		return err
	}
	cl, ok := pickClient(clients, r.client)
	if !ok {
		return errors.New("no attached tmux client")
	}
	instances, err := f.ListInstances()
	if err != nil {
		return err
	}
	if inst, ok := pickInstance(instances, cl.Session, r.alive); ok {
		if err := f.RequestSelect(inst.Pane, r.target); err == nil {
			if err := f.Reveal(cl.Name, inst.Pane); err == nil {
				return nil
			}
		}
		// The instance vanished between listing and use; open a new one.
	}
	r.open.target = tmux.LaunchTarget{Client: cl.Name, Pane: cl.Pane}
	if err := openDashboard(f, r.open); err != nil {
		f.Notify(r.open.target, "naviclaude: "+err.Error())
		return err
	}
	return nil
}

// pickClient returns the named client, else the most recently active one.
func pickClient(clients []tmux.ClientInfo, name string) (tmux.ClientInfo, bool) {
	var best tmux.ClientInfo
	found := false
	for _, c := range clients {
		if name != "" {
			if c.Name == name {
				return c, true
			}
			continue
		}
		if !found || c.Activity > best.Activity {
			best, found = c, true
		}
	}
	return best, found
}

// pickInstance prefers a marked (current) dashboard over one found only by its
// command, and within each the client's own session.
func pickInstance(instances []tmux.Instance, session string, alive func(int) bool) (tmux.Instance, bool) {
	rank := func(in tmux.Instance) int {
		r := 0
		if !in.Marked {
			r += 2
		}
		if in.Session != session {
			r++
		}
		return r
	}
	best, found := tmux.Instance{}, false
	for _, in := range instances {
		// An unmarked pane is listed only while its command is naviclaude, so it is live.
		if in.Marked && alive != nil && !alive(in.PID) {
			continue
		}
		if !found || rank(in) < rank(best) {
			best, found = in, true
		}
	}
	return best, found
}
