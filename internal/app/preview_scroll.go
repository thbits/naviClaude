package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/session"
)

// scrollRedrawDelay lets Claude Code redraw after a forwarded scroll (measured
// ~15ms past the send) so the follow-up capture sees the new frame.
const scrollRedrawDelay = 20 * time.Millisecond

// paneScrolledMsg reports a scroll forwarded to target, sent once it has
// likely redrawn.
type paneScrolledMsg struct{ target string }

// paneScroller sends scroll input to a pane; *tmux.Client in production.
type paneScroller interface {
	SendWheel(target string, up bool, x, y, n int) error
	SendKeys(target, keys string) error
}

func (m Model) paneScroller() paneScroller {
	if m.scroller != nil {
		return m.scroller
	}
	return m.tmuxClient
}

// selfScrollTarget is the previewed pane when it scrolls itself on wheel input
// (Claude Code keeps its transcript out of tmux history), else "" to scroll the
// preview's own viewport.
func (m Model) selfScrollTarget() string {
	sel := m.sidebar.SelectedSession()
	if sel == nil || sel.Status == session.StatusClosed || sel.Subprocess || sel.TmuxTarget != m.wheelTarget {
		return ""
	}
	return sel.TmuxTarget
}

// forwardWheel hands one wheel tick at screen cell (x, y) to a self-scrolling
// pane, so the app scrolls (and accelerates) as if wheeled in its own window.
func (m Model) forwardWheel(up bool, x, y int) (tea.Cmd, bool) {
	target := m.selfScrollTarget()
	if target == "" {
		return nil, false
	}
	// The pane is sized to the preview viewport, which sits below the title bar
	// and the preview header (2 rows) and right of its 1-column left padding.
	paneW, paneH := m.previewPaneDimensions()
	col := min(max(x-m.sidebarWidth(), 1), paneW)
	row := min(max(y-lipgloss.Height(m.renderTitleBar())-1, 1), paneH)
	s := m.paneScroller()
	return scrolledCmd(target, func() error { return s.SendWheel(target, up, col, row, 1) }), true
}

// forwardPage sends Ctrl+U/Ctrl+D to a self-scrolling pane as PageUp/PageDown,
// which Claude Code scrolls by half a screen; wheel bursts accelerate unevenly.
func (m Model) forwardPage(up bool) (tea.Cmd, bool) {
	target := m.selfScrollTarget()
	if target == "" {
		return nil, false
	}
	key := "PageDown"
	if up {
		key = "PageUp"
	}
	s := m.paneScroller()
	return scrolledCmd(target, func() error { return s.SendKeys(target, key) }), true
}

func scrolledCmd(target string, send func() error) tea.Cmd {
	return func() tea.Msg {
		if send() != nil {
			return nil
		}
		time.Sleep(scrollRedrawDelay)
		return paneScrolledMsg{target: target}
	}
}

// onPaneScrolled captures the preview right after a forwarded scroll rather
// than on the next tick. One such capture runs at a time; scrolls landing
// meanwhile queue a single follow-up.
func (m Model) onPaneScrolled(target string) (tea.Model, tea.Cmd) {
	if sel := m.sidebar.SelectedSession(); sel == nil || sel.TmuxTarget != target {
		return m, nil
	}
	if m.scrollCaptureInFlight {
		m.scrollCaptureDirty = true
		return m, nil
	}
	cmd := m.scrollCaptureCmd()
	m.scrollCaptureInFlight = cmd != nil
	return m, cmd
}

// finishScrollCapture ends the in-flight scroll capture and starts the queued one.
func (m *Model) finishScrollCapture() tea.Cmd {
	m.scrollCaptureInFlight = false
	if !m.scrollCaptureDirty {
		return nil
	}
	m.scrollCaptureDirty = false
	cmd := m.scrollCaptureCmd()
	m.scrollCaptureInFlight = cmd != nil
	return cmd
}

func (m Model) scrollCaptureCmd() tea.Cmd {
	capture := m.capturePreviewCmd()
	if capture == nil {
		return nil
	}
	return func() tea.Msg {
		msg := capture()
		if c, ok := msg.(previewCaptureMsg); ok {
			c.afterScroll = true
			return c
		}
		return msg
	}
}
