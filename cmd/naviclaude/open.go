package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/tmux"
)

// launcher is the tmux surface runOpen needs; *tmux.Client satisfies it.
type launcher interface {
	QueryServer(t tmux.LaunchTarget) (tmux.ServerInfo, error)
	IsLauncherPane(pane string) bool
	OpenFloatingPane(o tmux.FloatingPaneOptions) (string, error)
	DisplayPopup(o tmux.DisplayPopupOptions) error
	Notify(t tmux.LaunchTarget, msg string)
}

// runOpen opens the dashboard over the current window: a modal floating pane
// on tmux 3.8+, display-popup otherwise.
func runOpen(args []string) int {
	fs := flag.NewFlagSet("naviclaude open", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: naviclaude open [-c client] [-t pane] [-popup]")
		fmt.Fprintln(os.Stderr, "\nOpen naviClaude as a modal floating pane (tmux 3.8+) or a popup (older tmux).")
		fmt.Fprintln(os.Stderr, "Bind it with: bind g run-shell -b 'naviclaude open -c \"#{client_name}\" -t \"#{pane_id}\"'")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	client := fs.String("c", "", "target tmux client (pass #{client_name} from run-shell)")
	pane := fs.String("t", "", "target tmux pane (pass #{pane_id} from run-shell; defaults to $TMUX_PANE)")
	forcePopup := fs.Bool("popup", false, "use display-popup even when floating panes are available")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "naviclaude open must run inside tmux (e.g. from a run-shell binding).")
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude: resolve own path:", err)
		return 1
	}
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude: config:", err)
	}
	target := tmux.LaunchTarget{Client: *client, Pane: *pane}
	if target.Pane == "" {
		target.Pane = os.Getenv("TMUX_PANE")
	}
	req := openRequest{
		target:     target,
		command:    tmux.ShellQuote(exe),
		widthPct:   cfg.PopupWidth,
		heightPct:  cfg.PopupHeight,
		forcePopup: *forcePopup,
	}
	l := tmux.New()
	if err := openDashboard(l, req); err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude:", err)
		l.Notify(target, "naviclaude: "+err.Error())
		return 1
	}
	return 0
}

type openRequest struct {
	target              tmux.LaunchTarget
	command             string
	widthPct, heightPct int
	forcePopup          bool
}

var errOtherModal = errors.New("this window already has a modal pane")

func openDashboard(l launcher, r openRequest) error {
	info, err := l.QueryServer(r.target)
	if err != nil {
		return err
	}
	if !r.forcePopup && tmux.SupportsFloatingPanes(info.Version) {
		if info.ModalPane != "" {
			// A second press while the dashboard is up is a no-op.
			if l.IsLauncherPane(info.ModalPane) {
				return nil
			}
			return errOtherModal
		}
		_, err := l.OpenFloatingPane(tmux.FloatingPaneOptions{
			Target:        r.target,
			WidthPercent:  r.widthPct,
			HeightPercent: r.heightPct,
			WindowWidth:   info.WindowWidth,
			WindowHeight:  info.WindowHeight,
			Title:         "naviClaude",
			Command:       r.command,
		})
		if err == nil {
			return nil
		}
		// Fall through: a popup still works when new-pane is refused.
	}
	return l.DisplayPopup(tmux.DisplayPopupOptions{
		Target:  r.target,
		Width:   strconv.Itoa(r.widthPct) + "%",
		Height:  strconv.Itoa(r.heightPct) + "%",
		Command: r.command,
		Version: info.Version,
	})
}
