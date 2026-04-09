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
	// capture-pane omits the cursor line at the bottom of the pane, making
	// the output 1 line shorter than the actual pane height. TrimSuffix
	// removes the trailing newline so Split doesn't add a spurious element,
	// then we append an empty line to restore the correct total line count.
	raw = strings.TrimSuffix(raw, "\n")
	lines := strings.Split(raw, "\n")
	lines = append(lines, "")

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
