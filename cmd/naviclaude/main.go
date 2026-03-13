package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thbits/naviClaude/internal/app"
)

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
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

	m := app.New(version)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
