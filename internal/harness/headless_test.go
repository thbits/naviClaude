package harness

import (
	"testing"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/tmux"
)

// headlessHarness reports the PIDs in headless as non-interactive runs.
type headlessHarness struct {
	coreHarness
	headless map[int]bool
}

func (f *headlessHarness) Headless(pid int) bool { return f.headless[pid] }

func TestDetectSkipsHeadlessRuns(t *testing.T) {
	h := &headlessHarness{
		coreHarness: coreHarness{name: "claude", procs: map[string]bool{"claude": true}, enrichID: "id"},
		headless:    map[int]bool{21: true, 31: true},
	}
	tree := session.NewProcessTree([]session.Proc{
		{PID: 10, PPID: 1, Name: "zsh"}, {PID: 11, PPID: 10, Name: "claude"},
		{PID: 20, PPID: 1, Name: "python3"}, {PID: 21, PPID: 20, Name: "claude"},
		{PID: 30, PPID: 1, Name: "zsh"}, {PID: 31, PPID: 30, Name: "claude"},
	})
	panes := &fakePanes{panes: []tmux.PaneInfo{
		{SessionName: "s", Target: "s:1.0", CurrentCommand: "zsh", PID: 10},
		// A script running claude -p, and claude -p typed at a shell prompt.
		{SessionName: "s", Target: "s:2.0", CurrentCommand: "python3", PID: 20},
		{SessionName: "s", Target: "s:3.0", CurrentCommand: "claude", PID: 30},
	}}
	got, err := newTestDetector(panes, tree, h).Detect()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PID != 11 {
		t.Fatalf("Detect = %+v, want only the interactive session (pid 11)", got)
	}
}
