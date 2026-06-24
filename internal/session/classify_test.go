package session

import "testing"

// Real-capture corpus. These approximate how current Claude Code renders each
// state in a tmux pane (after ANSI stripping). Sourced from the status-detection
// design research and validated live via the dev build.
func TestClassifyPaneContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    PaneSignal
	}{
		// -- WAITING: permission / confirmation / selection prompts ----------
		{
			name: "permission prompt numbered menu",
			content: "● Bash(rm -rf build)\n" +
				"  Remove the build directory\n\n" +
				"Do you want to proceed?\n" +
				"❯ 1. Yes\n" +
				"  2. Yes, and don't ask again this session\n" +
				"  3. No, and tell Claude what to do differently (esc)\n",
			want: SignalWaiting,
		},
		{
			name: "selection cursor on Yes",
			content: "Do you want to make this edit to app.go?\n" +
				"❯ Yes\n" +
				"  No\n",
			want: SignalWaiting,
		},
		{
			name:    "bracketed Y/n confirmation",
			content: "Continue with installation? [Y/n]",
			want:    SignalWaiting,
		},
		{
			name:    "bracketed y/N confirmation",
			content: "Overwrite existing file? [y/N]",
			want:    SignalWaiting,
		},
		{
			name: "trust dialog",
			content: "Do you trust the files in this folder?\n" +
				"❯ 1. Yes, proceed\n" +
				"  2. No, exit\n",
			want: SignalWaiting,
		},
		{
			name: "plan mode would you like to proceed",
			content: "Here is the plan.\n\n" +
				"Would you like to proceed?\n" +
				"❯ 1. Yes, and auto-accept edits\n" +
				"  2. Yes, and manually approve edits\n" +
				"  3. No, keep planning\n",
			want: SignalWaiting,
		},
		{
			name:    "enter to select menu",
			content: "Use arrow keys to navigate.\nPress Enter to select.",
			want:    SignalWaiting,
		},
		{
			name:    "legacy what should Claude do instead",
			content: "Request interrupted.\nWhat should Claude do instead?",
			want:    SignalWaiting,
		},
		{
			name: "waiting prompt deep in scrollback within window",
			content: "Do you want to proceed?\n" +
				"❯ 1. Yes\n" +
				"  2. No\n" +
				"\n" +
				"  (Use arrow keys)\n" +
				"  Press ? for shortcuts\n",
			want: SignalWaiting,
		},

		// -- WORKING: the "esc to interrupt" footer --------------------------
		{
			name:    "spinner with esc to interrupt",
			content: "✻ Cogitating… (esc to interrupt)",
			want:    SignalWorking,
		},
		{
			name:    "working with token meter",
			content: "✶ Forging… (12s · ↓ 1.2k tokens · esc to interrupt)",
			want:    SignalWorking,
		},
		{
			name:    "ctrl+c to interrupt variant",
			content: "Running tests… (ctrl+c to interrupt)",
			want:    SignalWorking,
		},
		{
			name:    "interrupt footer with todos hint",
			content: "● Editing files\n✻ Working… (esc to interrupt · ctrl+t to hide todos)",
			want:    SignalWorking,
		},

		// -- NONE: idle input box / plain output -----------------------------
		{
			name: "idle input box",
			content: "╭───────────────────────────────────────╮\n" +
				"│ > Try \"edit <filepath>\"                │\n" +
				"╰───────────────────────────────────────╯\n" +
				"  ? for shortcuts",
			want: SignalNone,
		},
		{
			name:    "plain tool output no footer",
			content: "Building project...\nCompiling main.go\nDone.",
			want:    SignalNone,
		},
		{
			name:    "empty content",
			content: "",
			want:    SignalNone,
		},
		{
			name: "idle box must not match waiting on the prompt glyph",
			content: "╭───────────────────────────────────────╮\n" +
				"│ ❯ Try a command                         │\n" +
				"╰───────────────────────────────────────╯",
			want: SignalNone,
		},

		// -- WAITING: free-form selection menus (Claude question prompt) -----
		{
			name: "free-form selection menu with text options",
			content: "Which approach should we take?\n" +
				"❯ Stabilize, keep the look\n" +
				"  Stabilize + visual refresh\n" +
				"  You propose it\n",
			want: SignalWaiting,
		},
		{
			name: "free-form menu cursor on a later option",
			content: "Pick a theme:\n" +
				"  tokyo-night\n" +
				"❯ catppuccin\n" +
				"  gruvbox\n",
			want: SignalWaiting,
		},

		// -- NONE: ❯ that is NOT a selection menu ----------------------------
		{
			name: "typed text in bordered input is not a menu",
			content: "╭─────────────────────────────╮\n" +
				"│ ❯ fix the auth bug          │\n" +
				"╰─────────────────────────────╯\n" +
				"  ? for shortcuts",
			want: SignalNone,
		},
		{
			name:    "lone cursor line without sibling options is not a menu",
			content: "Running build\n❯ deploy to staging\nbuild finished",
			want:    SignalNone,
		},

		// -- priority: waiting beats working ---------------------------------
		{
			name: "waiting prompt takes priority over a stray interrupt line",
			content: "✻ Working… (esc to interrupt)\n" +
				"Do you want to proceed?\n" +
				"❯ 1. Yes\n" +
				"  2. No\n",
			want: SignalWaiting,
		},

		// -- ANSI sequences are stripped before matching ---------------------
		{
			name:    "waiting with ANSI color codes",
			content: "\x1b[1mDo you want to proceed?\x1b[0m\n\x1b[36m❯ 1. Yes\x1b[0m\n  2. No",
			want:    SignalWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyPaneContent(tt.content)
			if got != tt.want {
				t.Errorf("ClassifyPaneContent() = %v, want %v", got, tt.want)
			}
		})
	}
}
