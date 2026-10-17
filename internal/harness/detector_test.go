package harness

import (
	"errors"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/tmux"
)

// fakePanes is a PaneSource backed by fixed panes and pane content.
type fakePanes struct {
	panes    []tmux.PaneInfo
	content  string
	captured []string
}

func (f *fakePanes) ListPanes() ([]tmux.PaneInfo, error) { return f.panes, nil }
func (f *fakePanes) CapturePaneOutput(target string) (string, error) {
	f.captured = append(f.captured, target)
	return f.content, nil
}

func newTestDetector(panes PaneSource, tree *session.ProcessTree, hs ...Harness) *Detector {
	d := NewDetector(panes, NewRegistry(hs...), 0, 0)
	d.buildTree = func() *session.ProcessTree { return tree }
	return d
}

func statusPtr(s session.SessionStatus) *session.SessionStatus { return &s }

func TestNewDetectorDefaults(t *testing.T) {
	reg := NewRegistry(&coreHarness{name: "a"})

	d := NewDetector(nil, reg, 0, 0)
	if d.activeWindow != 5*time.Second {
		t.Errorf("activeWindow = %v, want 5s", d.activeWindow)
	}
	if d.cpuThreshold != 5.0 {
		t.Errorf("cpuThreshold = %v, want 5.0", d.cpuThreshold)
	}
	if d.tracker == nil {
		t.Error("expected non-nil status tracker")
	}

	d = NewDetector(nil, reg, -1, 0)
	if d.activeWindow != 5*time.Second {
		t.Errorf("negative active window: got %v, want 5s", d.activeWindow)
	}

	d = NewDetector(nil, reg, 10, 25.0)
	if d.activeWindow != 10*time.Second || d.cpuThreshold != 25.0 {
		t.Errorf("custom values: window=%v cpu=%v", d.activeWindow, d.cpuThreshold)
	}
}

func TestSessionFromPaneVersionTitle(t *testing.T) {
	const panePID = 56273
	h := &coreHarness{name: "claude", procs: map[string]bool{"claude": true}}
	tree := session.NewProcessTree([]session.Proc{{PID: panePID, PPID: 1, Name: "claude"}})
	d := newTestDetector(&fakePanes{}, tree, h)

	t.Run("detects the agent when tmux reports a retitled process", func(t *testing.T) {
		pane := tmux.PaneInfo{SessionName: "work", Target: "work:3.1", CurrentCommand: "2.1.191", PID: panePID, CurrentPath: "/tmp"}
		_, s := d.sessionFromPane(pane, tree)
		if s == nil {
			t.Fatal("sessionFromPane returned nil; want a session (agent is the pane's own process)")
		}
		if s.PID != panePID {
			t.Errorf("session PID = %d, want %d", s.PID, panePID)
		}
	})

	t.Run("plain shell pane is still ignored", func(t *testing.T) {
		shellTree := session.NewProcessTree([]session.Proc{{PID: 100, PPID: 1, Name: "zsh"}})
		pane := tmux.PaneInfo{Target: "work:4.1", CurrentCommand: "zsh", PID: 100, CurrentPath: "/tmp"}
		if _, s := d.sessionFromPane(pane, shellTree); s != nil {
			t.Errorf("sessionFromPane = %+v, want nil for a plain shell pane", s)
		}
	})
}

func TestDetectOwnershipAndStamping(t *testing.T) {
	a := &coreHarness{name: "a", procs: map[string]bool{"both": true, "only-a": true}, enrichID: "id-a"}
	b := &coreHarness{name: "b", procs: map[string]bool{"both": true, "only-b": true}, enrichID: "id-b"}
	tree := session.NewProcessTree([]session.Proc{
		{PID: 10, PPID: 1, Name: "zsh"}, {PID: 11, PPID: 10, Name: "both", CPU: 1.5, RSSKB: 2048},
		{PID: 20, PPID: 1, Name: "zsh"}, {PID: 21, PPID: 20, Name: "zsh"}, {PID: 22, PPID: 21, Name: "only-b"},
		{PID: 30, PPID: 1, Name: "zsh"},
	})
	panes := &fakePanes{panes: []tmux.PaneInfo{
		{SessionName: "s", Target: "s:1.0", CurrentCommand: "zsh", PID: 10, CurrentPath: "/x/proj"},
		{SessionName: "s", Target: "s:2.0", CurrentCommand: "zsh", PID: 20, CurrentPath: "/y"},
		{SessionName: "s", Target: "s:3.0", CurrentCommand: "zsh", PID: 30, CurrentPath: "/z"},
		{SessionName: "[popup]", Target: "p:1.0", CurrentCommand: "both", PID: 99},
	}}
	d := newTestDetector(panes, tree, a, b)

	got, err := d.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Detect returned %d sessions, want 2 (shell and popup panes skipped)", len(got))
	}
	first, second := got[0], got[1]
	if first.Harness != "a" || first.ID != "id-a" || first.PID != 11 {
		t.Errorf("first registered harness should own a pane both match: %+v", first)
	}
	if first.ProjectName != "proj" || first.CPU != 1.5 || first.Memory != 2 {
		t.Errorf("base fields not filled: project=%q cpu=%v mem=%v", first.ProjectName, first.CPU, first.Memory)
	}
	if second.Harness != "b" || second.PID != 22 {
		t.Errorf("descendant walk should find b's process: %+v", second)
	}
}

func TestResolveStatusNativeShortCircuits(t *testing.T) {
	h := &coreHarness{name: "a", native: statusPtr(session.StatusWaiting), signal: session.SignalWorking}
	tree := session.NewProcessTree([]session.Proc{{PID: 42, PPID: 1, Name: "a", CPU: 99}})
	panes := &fakePanes{}
	d := newTestDetector(panes, tree, h)

	got := d.ResolveStatus(h, &session.Session{PID: 42, TmuxTarget: "s:1.0"}, tree)
	if got != session.StatusWaiting {
		t.Errorf("ResolveStatus = %v, want native Waiting despite CPU", got)
	}
	if len(panes.captured) != 0 || h.classifyN != 0 {
		t.Errorf("native status must skip pane capture; captured=%v classify=%d", panes.captured, h.classifyN)
	}
}

func TestResolveStatusFallback(t *testing.T) {
	busy := session.NewProcessTree([]session.Proc{{PID: 42, PPID: 1, Name: "a", CPU: 99}})
	idle := session.NewProcessTree([]session.Proc{{PID: 42, PPID: 1, Name: "a"}})

	tests := []struct {
		name   string
		h      *coreHarness
		tree   *session.ProcessTree
		target string
		want   session.SessionStatus
	}{
		{"pane prompt is waiting", &coreHarness{name: "a", signal: session.SignalWaiting}, idle, "s:1.0", session.StatusWaiting},
		{"working footer is active", &coreHarness{name: "a", signal: session.SignalWorking}, idle, "s:1.0", session.StatusActive},
		{"cpu above threshold is active", &coreHarness{name: "a"}, busy, "", session.StatusActive},
		{"recent write is active", &coreHarness{name: "a", recent: true}, idle, "", session.StatusActive},
		{"no signal is idle", &coreHarness{name: "a"}, idle, "", session.StatusIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestDetector(&fakePanes{}, tt.tree, tt.h)
			s := &session.Session{PID: 42, TmuxTarget: tt.target}
			if got := d.ResolveStatus(tt.h, s, tt.tree); got != tt.want {
				t.Errorf("ResolveStatus = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveStatusSubprocessSkipsCapture(t *testing.T) {
	h := &coreHarness{name: "a", signal: session.SignalWaiting}
	tree := session.NewProcessTree([]session.Proc{{PID: 42, PPID: 1, Name: "a"}})
	panes := &fakePanes{}
	d := newTestDetector(panes, tree, h)

	s := &session.Session{PID: 42, TmuxTarget: "s:1.0", Subprocess: true}
	if got := d.ResolveStatus(h, s, tree); got != session.StatusIdle {
		t.Errorf("ResolveStatus = %v, want Idle (subprocess pane content is not the agent's)", got)
	}
	if len(panes.captured) != 0 {
		t.Errorf("subprocess pane must not be captured, got %v", panes.captured)
	}
}

type errPanes struct{}

func (errPanes) ListPanes() ([]tmux.PaneInfo, error)      { return nil, errors.New("no server") }
func (errPanes) CapturePaneOutput(string) (string, error) { return "", nil }

func TestDetectListPanesError(t *testing.T) {
	d := newTestDetector(errPanes{}, session.NewProcessTree(nil), &coreHarness{name: "a"})
	if _, err := d.Detect(); err == nil {
		t.Error("Detect should surface ListPanes errors")
	}
}

func TestPreviewStatus(t *testing.T) {
	native := &coreHarness{name: "a", native: statusPtr(session.StatusIdle), signal: session.SignalWaiting}
	if got := PreviewStatus(native, &session.Session{}, ""); got != session.StatusIdle {
		t.Errorf("native status should win, got %v", got)
	}
	prompt := &coreHarness{name: "a", signal: session.SignalWaiting}
	if got := PreviewStatus(prompt, &session.Session{}, ""); got != session.StatusWaiting {
		t.Errorf("prompt should be Waiting, got %v", got)
	}
	quiet := &coreHarness{name: "a"}
	if got := PreviewStatus(quiet, &session.Session{}, ""); got != session.StatusActive {
		t.Errorf("no prompt should be the Active sentinel, got %v", got)
	}
}

func TestDetectSkipsOwnPane(t *testing.T) {
	h := &coreHarness{name: "claude", procs: map[string]bool{"claude": true}}
	panes := &fakePanes{panes: []tmux.PaneInfo{
		// naviClaude's floating pane: sh -c naviclaude, with a matching child.
		{SessionName: "s", Target: "s:1.1", PaneID: "%9", CurrentCommand: "naviclaude", PID: 500},
		{SessionName: "s", Target: "s:1.0", PaneID: "%1", CurrentCommand: "claude", PID: 600},
	}}
	cases := map[string]*session.ProcessTree{
		"self in ps snapshot": session.NewProcessTree([]session.Proc{
			{PID: 500, PPID: 1, Name: "sh"}, {PID: 501, PPID: 500, Name: "naviclaude"},
			{PID: 502, PPID: 501, Name: "claude"}, {PID: 600, PPID: 1, Name: "claude"},
		}),
		"self missing from ps snapshot": session.NewProcessTree([]session.Proc{
			{PID: 500, PPID: 1, Name: "sh"}, {PID: 502, PPID: 500, Name: "claude"},
			{PID: 600, PPID: 1, Name: "claude"},
		}),
	}
	for name, tree := range cases {
		t.Run(name, func(t *testing.T) {
			d := newTestDetector(panes, tree, h)
			d.selfPID, d.parentPID = 501, 500
			got, err := d.Detect()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].TmuxPaneID != "%1" {
				t.Fatalf("Detect = %d sessions (%+v), want only %%1", len(got), got)
			}
		})
	}
}
