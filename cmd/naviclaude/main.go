package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/app"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/tmux"
)

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	// Subcommands run without tmux checks: hooks are invoked by Claude Code.
	if len(os.Args) > 1 {
		if handled, code := runSubcommand(os.Args[1:]); handled {
			os.Exit(code)
		}
	}

	// Parse flags with the stdlib flag package: -v/--version anywhere,
	// -h/--help for usage, and graceful rejection of unknown flags.
	fs := flag.NewFlagSet("naviclaude", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: naviclaude [flags]")
		fmt.Fprintln(os.Stderr, "       naviclaude open [-c client] [-t pane] [-popup]")
		fmt.Fprintln(os.Stderr, "       naviclaude hooks install|uninstall|status")
		fmt.Fprintln(os.Stderr, "       naviclaude sound request|done")
		fmt.Fprintln(os.Stderr, "\nA tmux-native dashboard for navigating Claude sessions.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	showVersion := fs.Bool("version", false, "print version and exit")
	fs.BoolVar(showVersion, "v", false, "print version and exit (shorthand)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		// flag already printed the error and usage for -h/--help or unknown
		// flags. Exit 0 for an explicit help request, 2 otherwise.
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	if *showVersion {
		fmt.Println("naviclaude", version)
		return
	}

	// Check that tmux is installed.
	if _, err := exec.LookPath("tmux"); err != nil {
		fmt.Fprintln(os.Stderr, "naviClaude requires tmux but it was not found in your PATH.")
		fmt.Fprintln(os.Stderr, "Install it with: brew install tmux")
		fmt.Fprintln(os.Stderr, "More info: https://github.com/tmux/tmux/wiki")
		os.Exit(1)
	}

	// Check that we're running inside a tmux session.
	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "naviClaude must be run inside a tmux session.")
		fmt.Fprintln(os.Stderr, "Start one with: tmux")
		os.Exit(1)
	}

	// tmux 3.8 greys display-popups by default; naviClaude draws on the terminal's colours.
	var tree *session.ProcessTree
	tmux.New().RestyleOwnPopup(func(pid int) (int, bool) {
		if tree == nil {
			tree = session.BuildProcessTree()
		}
		return tree.Parent(pid)
	})

	m := app.New(version)
	// Cell motion covers presses, drags and wheel without AllMotion's event flood,
	// which also blocks terminal text selection.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
