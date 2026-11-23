package main

import (
	"fmt"
	"os"
	"os/exec"
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
		if len(args) >= 5 && args[1] == "--watch" {
			attention.RunWatcher(args[2], args[3], args[4], args[5:]...)
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
	case "notify":
		return true, runNotify(args[1:])
	case "open":
		return true, runOpen(args[1:])
	case "focus":
		return true, runFocus(args[1:])
	case "list", "wait", "send", "skill":
		return true, runScripting(args[0], args[1:])
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
		exe = attention.StableExecutable(exe, exec.LookPath)
		if err := attention.InstallHooks(path, exe); err != nil {
			fmt.Fprintln(os.Stderr, "naviclaude: install hooks:", err)
			return 1
		}
		fmt.Printf("Installed naviClaude hooks (v%d) in %s\n  command: %s\nRestart running Claude sessions to pick them up.\n", attention.HookVersion, path, attention.HookCommand(exe))
		if cfg, _ := config.Load(""); cfg.Notifications.Desktop == notify.DesktopSystem {
			if hint := notify.NewNotifier(notify.DesktopSystem, true).SetupHint(); hint != "" {
				fmt.Println(hint)
			}
		}
	case "uninstall":
		if err := attention.UninstallHooks(path); err != nil {
			fmt.Fprintln(os.Stderr, "naviclaude: uninstall hooks:", err)
			return 1
		}
		fmt.Printf("Removed naviClaude hooks from %s\n", path)
	case "status":
		return hooksStatus(path)
	default:
		fmt.Fprintln(os.Stderr, "usage: naviclaude hooks install|uninstall|status")
		return 2
	}
	return 0
}

// hooksStatus prints the hook state; nonzero when hooks are outdated or broken.
func hooksStatus(path string) int {
	r, err := attention.CheckHooks(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude:", err)
		return 1
	}
	switch r.State {
	case attention.HooksNotInstalled:
		fmt.Println("naviClaude hooks: not installed (run: naviclaude hooks install)")
		return 0
	case attention.HooksCurrent:
		fmt.Printf("naviClaude hooks: installed, current (v%d) for %s\n  command: %s\n", r.Version, strings.Join(r.Events, ", "), r.Command)
		return 0
	case attention.HooksOutdated:
		if r.Version < attention.HookVersion {
			fmt.Printf("naviClaude hooks: installed but outdated (v%d, current is v%d)\n", r.Version, attention.HookVersion)
		} else {
			fmt.Println("naviClaude hooks: installed but outdated")
		}
	case attention.HooksBroken:
		fmt.Println("naviClaude hooks: installed but broken")
	}
	fmt.Printf("  command: %s\n", r.Command)
	for _, p := range r.Problems {
		fmt.Printf("  - %s\n", p)
	}
	fmt.Println("  fix: naviclaude hooks install")
	return 1
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

func runNotify(args []string) int {
	if len(args) == 0 || args[0] != "test" || len(args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: naviclaude notify test [request|done] [system|terminal]")
		return 2
	}
	kind, mode := "request", ""
	for _, a := range args[1:] {
		switch a {
		case "request", "done":
			kind = a
		case notify.DesktopSystem, notify.DesktopTerminal:
			mode = a
		default:
			fmt.Fprintln(os.Stderr, "usage: naviclaude notify test [request|done] [system|terminal]")
			return 2
		}
	}
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude: config:", err)
	}
	if mode == "" {
		mode = cfg.Notifications.Desktop
		if mode == notify.DesktopOff {
			fmt.Println("notifications.desktop is off; testing with system")
			mode = notify.DesktopSystem
		}
	}
	b := notify.Banner{Title: "naviClaude test", Subtitle: "naviClaude ⎇ main", Message: "Approve Bash: go test ./...", Pane: os.Getenv("TMUX_PANE")}
	if kind == "done" {
		b.Message = "All tests pass; the banner now shows the session, its branch and Claude's reply."
	}
	n := notify.NewNotifier(mode, true)
	n.Click = cfg.Notifications.Click // the same click path as a real alert
	if err := n.Notify(b); err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude:", err)
		return 1
	}
	if n.Fallback != nil {
		fmt.Printf("Sent a %s banner via %s, because terminal-notifier failed:\n  %v\n", mode, fallbackName(n), n.Fallback)
	} else {
		fmt.Printf("Sent a %s banner via %s.\n", mode, n.Backend())
	}
	if hint := n.SetupHint(); hint != "" {
		fmt.Println(hint)
	}
	if mode == notify.DesktopTerminal && os.Getenv("TMUX") != "" {
		out, _ := exec.Command("tmux", "display-message", "-p", "#{allow-passthrough}").Output()
		if v := strings.TrimSpace(string(out)); v != "on" && v != "all" {
			fmt.Printf("tmux allow-passthrough is %q, so tmux drops it; add to tmux.conf: set -g allow-passthrough on\n", v)
		}
	}
	return 0
}

// fallbackName says which backend a failed terminal-notifier banner fell back to.
func fallbackName(n *notify.Notifier) string {
	if n.Mode == notify.DesktopSystem && n.FallsBackToTerminal() {
		return "your terminal"
	}
	return "osascript"
}
