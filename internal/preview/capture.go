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
}

// NewCaptureEngine creates a CaptureEngine backed by the given tmux client.
func NewCaptureEngine(client *tmux.Client) *CaptureEngine {
	return &CaptureEngine{tmuxClient: client}
}

// cursorReverseOn/Off draw a block cursor: reverse video turns a blank cell into
// a solid block.
const (
	cursorReverseOn  = "\x1b[7m"
	cursorReverseOff = "\x1b[27m"
)

// Capture returns target's ANSI pane content cut to maxWidth (0 = none; a param so
// it can't race a resize); showCursor overlays the cursor capture-pane omits.
func (e *CaptureEngine) Capture(target string, showCursor bool, maxWidth int) (string, error) {
	raw, err := e.tmuxClient.CapturePaneOutput(target)
	if err != nil {
		return "", err
	}
	raw = strings.TrimSuffix(raw, "\n")
	lines := strings.Split(raw, "\n")

	// The untrimmed count (history + visible rows) maps the cursor to line
	// rawCount - paneHeight + cursorY.
	rawCount := len(lines)

	// Trailing blanks below top-anchored content (e.g. a trust prompt) would push it
	// out of the bottom-following viewport; trimming the end keeps cursor rows valid.
	lines = trimTrailingBlankLines(lines)

	if maxWidth > 0 {
		// Only a safety net for stale scrollback; resizing the pane does the real fit.
		for i, line := range lines {
			if ansi.StringWidth(line) > maxWidth {
				lines[i] = ansi.Truncate(line, maxWidth, "")
			}
		}
	}

	if showCursor {
		lines = e.overlayCursor(target, lines, rawCount, maxWidth)
	}

	return strings.Join(lines, "\n"), nil
}

// overlayCursor draws target's live cursor as a reverse-video block; a cursor on a
// trimmed trailing row is skipped for the frame rather than drawn on the wrong one.
func (e *CaptureEngine) overlayCursor(target string, lines []string, rawCount, maxWidth int) []string {
	info, err := e.tmuxClient.CursorPosition(target)
	if err != nil || !info.Visible || info.PaneHeight <= 0 {
		return lines
	}
	row := rawCount - info.PaneHeight + info.Y
	return drawCursor(lines, row, info.X, maxWidth)
}

// drawCursor reverse-videos column x of lines[row], skipping cells outside the
// content or past maxWidth (0 = unbounded).
func drawCursor(lines []string, row, x, maxWidth int) []string {
	if row < 0 || row >= len(lines) || x < 0 {
		return lines
	}
	if maxWidth > 0 && x >= maxWidth {
		return lines
	}
	lines[row] = renderCursorOnLine(lines[row], x)
	return lines
}

// renderCursorOnLine reverse-videos the cell at display column x, padding the
// line when the cursor sits past the text (e.g. an empty prompt).
func renderCursorOnLine(line string, x int) string {
	width := ansi.StringWidth(line)
	if x >= width {
		pad := strings.Repeat(" ", x-width)
		return line + "\x1b[0m" + pad + cursorReverseOn + " " + cursorReverseOff
	}
	left := ansi.Truncate(line, x, "")
	right := ansi.TruncateLeft(line, x+1, "")
	cell := ansi.Strip(ansi.Cut(line, x, x+1))
	if cell == "" {
		cell = " "
	}
	return left + cursorReverseOn + cell + cursorReverseOff + right
}

// trimTrailingBlankLines drops trailing lines holding only whitespace and ANSI.
func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(ansi.Strip(lines[end-1])) == "" {
		end--
	}
	return lines[:end]
}
