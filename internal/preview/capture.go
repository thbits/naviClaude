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
	lines := strings.Split(raw, "\n")

	// Trim trailing blank lines so GotoBottom doesn't scroll past
	// the actual content into empty space.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	if e.maxWidth > 0 {
		for i, line := range lines {
			if ansi.StringWidth(line) > e.maxWidth {
				lines[i] = ansi.Truncate(line, e.maxWidth, "")
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}
