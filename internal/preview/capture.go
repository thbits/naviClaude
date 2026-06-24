package preview

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/tmux"
)

// CaptureEngine wraps the tmux client to capture pane content with ANSI
// escape sequences preserved.
type CaptureEngine struct {
	tmuxClient *tmux.Client
	maxWidth   int
}

// NewCaptureEngine creates a CaptureEngine backed by the given tmux client.
func NewCaptureEngine(client *tmux.Client) *CaptureEngine {
	return &CaptureEngine{tmuxClient: client}
}

// SetMaxWidth sets the maximum line width for captured content. Lines wider
// than this are truncated using ANSI-aware truncation to prevent layout overflow.
func (e *CaptureEngine) SetMaxWidth(w int) {
	e.maxWidth = w
}

// Capture returns the raw pane content including ANSI escape sequences for the
// given tmux target (e.g. "session:1.0"). Lines are truncated to maxWidth if set.
func (e *CaptureEngine) Capture(target string) (string, error) {
	raw, err := e.tmuxClient.CapturePaneOutput(target)
	if err != nil {
		return "", err
	}
	raw = strings.TrimSuffix(raw, "\n")
	lines := strings.Split(raw, "\n")

	// Drop trailing blank lines. capture-pane -S - returns the full pane grid,
	// so a pane taller than its current content yields many trailing blank
	// lines -- most notably Claude Code's trust/permission prompts, which render
	// at the top of the pane and leave the rest empty. The preview viewport
	// auto-scrolls to the bottom to follow live output; without trimming, those
	// blanks push the prompt off the top and the preview looks empty. For a
	// normal bottom-anchored session this is a no-op (its content already ends
	// at the bottom). When the whole pane is blank this collapses to nothing, so
	// the caller surfaces its "waiting for output" placeholder.
	lines = trimTrailingBlankLines(lines)

	if e.maxWidth > 0 {
		// Truncate lines that exceed the preview viewport width. This is
		// just a safety net for stale scrollback content. The primary
		// mechanism is resizing the tmux pane to match the viewport so
		// the app inside (Claude, neovim, etc.) re-renders at the correct
		// width via SIGWINCH.
		for i, line := range lines {
			if ansi.StringWidth(line) > e.maxWidth {
				lines[i] = ansi.Truncate(line, e.maxWidth, "")
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}

// trimTrailingBlankLines returns lines with trailing blank lines removed. A line
// is blank when it holds nothing but whitespace and ANSI escape sequences.
// Interior blank lines are preserved.
func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(ansi.Strip(lines[end-1])) == "" {
		end--
	}
	return lines[:end]
}
