package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type scrollCall struct {
	target string
	up     bool
	x, y   int
	n      int
	keys   string
}

type fakeScroller struct{ calls []scrollCall }

func (f *fakeScroller) SendWheel(target string, up bool, x, y, n int) error {
	f.calls = append(f.calls, scrollCall{target: target, up: up, x: x, y: y, n: n})
	return nil
}

func (f *fakeScroller) SendKeys(target, keys string) error {
	f.calls = append(f.calls, scrollCall{target: target, keys: keys})
	return nil
}

// scrollModel is clickModel with a1 selected and a fake scroller wired in.
func scrollModel(t *testing.T, wheelTarget string) (*Model, *fakeScroller) {
	t.Helper()
	m, _ := clickModel(t)
	if selectedID(m) != "a1" {
		t.Fatalf("precondition: selected = %q, want a1", selectedID(m))
	}
	f := &fakeScroller{}
	m.scroller = f
	m.wheelTarget = wheelTarget
	return m, f
}

func run(cmd tea.Cmd) {
	if cmd != nil {
		cmd()
	}
}

func TestPreviewWheelForwardsToSelfScrollingPane(t *testing.T) {
	m, f := scrollModel(t, "zzmouse:0.1")
	run(press(m, tea.MouseButtonWheelUp, 50, 10))
	run(press(m, tea.MouseButtonWheelDown, 50, 10))
	// Preview starts at x=30 with 1 column of padding; its viewport starts below
	// the 1-row title bar and the 2-row preview header.
	want := []scrollCall{
		{target: "zzmouse:0.1", up: true, x: 20, y: 8, n: 1},
		{target: "zzmouse:0.1", up: false, x: 20, y: 8, n: 1},
	}
	if len(f.calls) != len(want) {
		t.Fatalf("calls = %+v, want %+v", f.calls, want)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Errorf("call %d = %+v, want %+v", i, f.calls[i], want[i])
		}
	}
}

func TestPreviewWheelClampsToPane(t *testing.T) {
	m, f := scrollModel(t, "zzmouse:0.1")
	run(press(m, tea.MouseButtonWheelUp, 30, 1)) // the padding column and title row
	if len(f.calls) != 1 || f.calls[0].x != 1 || f.calls[0].y != 1 {
		t.Errorf("calls = %+v, want one at (1,1)", f.calls)
	}
}

func TestPreviewWheelScrollsLocallyOtherwise(t *testing.T) {
	for name, target := range map[string]string{
		"pane keeps tmux history": "",
		"stale capture target":    "zzmouse:0.2",
	} {
		t.Run(name, func(t *testing.T) {
			m, f := scrollModel(t, target)
			run(press(m, tea.MouseButtonWheelUp, 50, 10))
			if len(f.calls) != 0 {
				t.Errorf("forwarded %+v, want local scroll", f.calls)
			}
		})
	}
}

func TestPreviewHalfPageKeysForwardPageKeys(t *testing.T) {
	m, f := scrollModel(t, "zzmouse:0.1")
	for _, k := range []tea.KeyType{tea.KeyCtrlU, tea.KeyCtrlD} {
		next, cmd := m.handleKey(tea.KeyMsg{Type: k})
		*m = next.(Model)
		run(cmd)
	}
	if len(f.calls) != 2 || f.calls[0].keys != "PageUp" || f.calls[1].keys != "PageDown" || f.calls[0].target != "zzmouse:0.1" {
		t.Errorf("calls = %+v, want PageUp then PageDown to zzmouse:0.1", f.calls)
	}
}

func TestPreviewCaptureTracksWheelTarget(t *testing.T) {
	m, _ := scrollModel(t, "")
	capture := func(forwards bool) {
		next, _ := m.Update(previewCaptureMsg{content: "x", target: "zzmouse:0.1", sessionID: "a1", forwardsWheel: forwards})
		*m = next.(Model)
	}
	capture(true)
	if m.wheelTarget != "zzmouse:0.1" {
		t.Fatalf("wheelTarget = %q after forwarding capture, want zzmouse:0.1", m.wheelTarget)
	}
	capture(false)
	if m.wheelTarget != "" {
		t.Errorf("wheelTarget = %q after plain capture, want empty", m.wheelTarget)
	}
}

func TestForwardedScrollReportsPaneScrolled(t *testing.T) {
	m, _ := scrollModel(t, "zzmouse:0.1")
	cmd := press(m, tea.MouseButtonWheelUp, 50, 10)
	if msg, ok := cmd().(paneScrolledMsg); !ok || msg.target != "zzmouse:0.1" {
		t.Errorf("wheel cmd returned %#v, want paneScrolledMsg for zzmouse:0.1", msg)
	}
}

// A flick of scrolls runs one capture at a time, plus one more for scrolls that
// landed while it ran.
func TestPaneScrolledCoalescesCaptures(t *testing.T) {
	m, _ := scrollModel(t, "zzmouse:0.1")
	update := func(msg tea.Msg) tea.Cmd {
		next, cmd := m.Update(msg)
		*m = next.(Model)
		return cmd
	}
	scrolled := paneScrolledMsg{target: "zzmouse:0.1"}
	captured := previewCaptureMsg{content: "x", target: "zzmouse:0.1", sessionID: "a1", forwardsWheel: true, afterScroll: true}

	if update(scrolled) == nil || !m.scrollCaptureInFlight {
		t.Fatal("first scroll should start a capture")
	}
	if update(scrolled) != nil || update(scrolled) != nil || !m.scrollCaptureDirty {
		t.Fatal("scrolls during a capture should only mark it dirty")
	}
	if update(captured) == nil || !m.scrollCaptureInFlight || m.scrollCaptureDirty {
		t.Fatal("finishing a dirty capture should start exactly one follow-up")
	}
	if update(captured) != nil || m.scrollCaptureInFlight {
		t.Fatal("a clean capture should end the run")
	}
	if update(paneScrolledMsg{target: "zzmouse:0.2"}) != nil {
		t.Error("a scroll of a pane no longer previewed should not capture")
	}
}
