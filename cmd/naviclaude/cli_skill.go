package main

import (
	"fmt"
	"os"

	"github.com/thbits/naviClaude/skills"
)

// runScripting dispatches the commands that let scripts and agents drive sessions.
func runScripting(cmd string, args []string) int {
	switch cmd {
	case "list":
		return runList(args)
	case "wait":
		return runWait(args)
	case "send":
		return runSend(args)
	}
	return runSkill(args)
}

// runSkill prints the bundled Claude Code skill, for installing with a redirect.
func runSkill(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "usage: naviclaude skill   # e.g. naviclaude skill > ~/.claude/skills/naviclaude/SKILL.md")
		return 2
	}
	fmt.Print(skills.NaviClaude)
	return 0
}
