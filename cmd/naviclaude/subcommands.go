package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/notify"
)

// runSubcommand dispatches the non-TUI commands. handled is false for
// anything that should fall through to the dashboard's flag parsing.
func runSubcommand(args []string) (handled bool, code int) {
	switch args[0] {
	case "hook":
		if len(args) == 5 && args[1] == "--watch" {
			attention.RunWatcher(args[2], args[3], args[4])
			return true, 0
		}
		// Never fail the hook: a broken notifier must not disturb Claude.
		if err := attention.RunHook(os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "naviclaude hook:", err)
		}
		return true, 0
	case "hooks":
		return true, runHooks(args[1:])
	case "sound":
		return true, runSound(args[1:])
	}
	return false, 0
}

func runHooks(args []string) int {
	path := attention.SettingsPath()
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "naviclaude: resolve own path:", err)
			return 1
		}
		if err := attention.InstallHooks(path, exe); err != nil {
			fmt.Fprintln(os.Stderr, "naviclaude: install hooks:", err)
			return 1
		}
		fmt.Printf("Installed naviClaude hooks in %s\n  command: %s\nRestart running Claude sessions to pick them up.\n", path, attention.HookCommand(exe))
	case "uninstall":
		if err := attention.UninstallHooks(path); err != nil {
			fmt.Fprintln(os.Stderr, "naviclaude: uninstall hooks:", err)
			return 1
		}
		fmt.Printf("Removed naviClaude hooks from %s\n", path)
	case "status":
		events, cmd, err := attention.InstalledHooks(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "naviclaude:", err)
			return 1
		}
		if len(events) == 0 {
			fmt.Println("naviClaude hooks: not installed (run: naviclaude hooks install)")
			return 0
		}
		fmt.Printf("naviClaude hooks: installed for %s\n  command: %s\n", strings.Join(events, ", "), cmd)
	default:
		fmt.Fprintln(os.Stderr, "usage: naviclaude hooks install|uninstall|status")
		return 2
	}
	return 0
}

func runSound(args []string) int {
	if len(args) != 1 || (args[0] != string(notify.Request) && args[0] != string(notify.Done)) {
		fmt.Fprintln(os.Stderr, "usage: naviclaude sound request|done")
		return 2
	}
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude: config:", err)
	}
	n := cfg.Notifications
	custom := n.RequestSound
	if args[0] == string(notify.Done) {
		custom = n.DoneSound
	}
	if err := notify.Play(notify.Sound(args[0]), custom, n.Volume); err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude:", err)
		return 1
	}
	return 0
}
