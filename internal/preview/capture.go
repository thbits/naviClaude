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

// cursorReverseOn / cursorReverseOff toggle reverse video, which renders a
// terminal block cursor: the cell's foreground and background are swapped, so a
// blank cell becomes a solid block in the foreground color.
const (
	cursorReverseOn  = "\x1b[7m"
	cursorReverseOff = "\x1b[27m"
)

// Capture returns the raw pane content including ANSI escape sequences for the
// given tmux target (e.g. "session:1.0"). Lines are truncated to maxWidth if
// set. When showCursor is true the pane's cursor is overlaid as a reverse-video
// block so the preview shows where input lands (capture-pane omits the cursor);
// callers pass false for unfocused panes (e.g. while browsing the menu) so the
// block doesn't imply the pane is receiving input.
func (e *CaptureEngine) Capture(target string, showCursor bool) (string, error) {
	raw, err := e.tmuxClient.CapturePaneOutput(target)
	if err != nil {
		return "", err
	}
	raw = strings.TrimSuffix(raw, "\n")
	lines := strings.Split(raw, "\n")

	// rawCount is the full grid height (history + visible rows) before trimming.
	// capture-pane -S - returns history_size + pane_height lines, so the cursor's
	// absolute line index is rawCount - paneHeight + cursorY. Capturing it here,
	// before trimTrailingBlankLines, keeps the mapping consistent with this
	// capture even if the pane produces more output before the next refresh.
	rawCount := len(lines)

	// Drop trailing blank lines. capture-pane -S - returns the full pane grid,
	// so a pane taller than its current content yields many trailing blank
	// lines -- most notably Claude Code's trust/permission prompts, which render
	// at the top of the pane and leave the rest empty. The preview viewport
	// auto-scrolls to the bottom to follow live output; without trimming, those
	// blanks push the prompt off the top and the preview looks empty. For a
	// normal bottom-anchored session this is a no-op (its content already ends
	// at the bottom). When the whole pane is blank this collapses to nothing, so
	// the caller surfaces its "waiting for output" placeholder. Trimming only
	// removes lines from the end, so indices of earlier lines (including the
	// cursor row) are unchanged.
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

	if showCursor {
		lines = e.overlayCursor(target, lines, rawCount)
	}

	return strings.Join(lines, "\n"), nil
}

// overlayCursor fetches the live cursor position for target and draws a
// reverse-video block at that cell. rawCount is the untrimmed capture line
// count, used to map the pane-relative cursor row to an index in lines. Any
// error or a hidden cursor leaves the content unchanged.
func (e *CaptureEngine) overlayCursor(target string, lines []string, rawCount int) []string {
	info, err := e.tmuxClient.CursorPosition(target)
	if err != nil || !info.Visible || info.PaneHeight <= 0 {
		return lines
	}
	row := rawCount - info.PaneHeight + info.Y
	return drawCursor(lines, row, info.X, e.maxWidth)
}

// drawCursor renders a reverse-video block at column x of lines[row], returning
// lines unchanged when row or x fall outside the rendered content. maxWidth
// (0 = unbounded) guards against drawing past the truncated viewport width.
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

// renderCursorOnLine wraps the single cell at display column x of line in
// reverse video. When x is at or past the line's width (cursor sitting beyond
// the text, e.g. an empty shell prompt) the line is padded with spaces and the
// block is drawn on a trailing space. ANSI sequences and wide characters in the
// line are preserved.
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
