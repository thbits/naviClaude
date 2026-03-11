package preview

import (
	"github.com/tomhalo/naviclaude/internal/tmux"
)

// CaptureEngine wraps the tmux client to capture pane content with ANSI
// escape sequences preserved.
type CaptureEngine struct {
	tmuxClient *tmux.Client
}

// NewCaptureEngine creates a CaptureEngine backed by the given tmux client.
func NewCaptureEngine(client *tmux.Client) *CaptureEngine {
	return &CaptureEngine{tmuxClient: client}
}

// Capture returns the raw pane content including ANSI escape sequences for the
// given tmux target (e.g. "session:1.0"). The content is returned as-is for
// the Bubble Tea viewport to render.
func (e *CaptureEngine) Capture(target string) (string, error) {
	return e.tmuxClient.CapturePaneOutput(target)
}
