package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thbits/naviClaude/internal/tmux"
)

type fakeFocuser struct {
	fakeLauncher
	clients   []tmux.ClientInfo
	instances []tmux.Instance
	requested [][2]string
	revealed  [][2]string
	revealErr error
}

func (f *fakeFocuser) ListClients() ([]tmux.ClientInfo, error) { return f.clients, nil }
func (f *fakeFocuser) ListInstances() ([]tmux.Instance, error) { return f.instances, nil }
func (f *fakeFocuser) RequestSelect(pane, target string) error {
	f.requested = append(f.requested, [2]string{pane, target})
	return nil
}
func (f *fakeFocuser) Reveal(client, pane string) error {
	f.revealed = append(f.revealed, [2]string{client, pane})
	return f.revealErr
}

func focusReq() focusRequest {
	r := focusRequest{target: "%12", alive: func(int) bool { return true }, open: testRequest()}
	r.open.command = "'/bin/naviclaude' --select '%12'"
	return r
}

var twoClients = []tmux.ClientInfo{
	{Name: "/dev/ttys001", Activity: 100, Session: "work", Pane: "%1"},
	{Name: "/dev/ttys002", Activity: 200, Session: "play", Pane: "%2"},
}

func TestFocusHandsOffToRunningInstanceInClientSession(t *testing.T) {
	f := &fakeFocuser{clients: twoClients, instances: []tmux.Instance{
		{Pane: "%5", Session: "work", PID: 1, Marked: true}, {Pane: "%6", Session: "play", PID: 2, Marked: true}}}
	if err := focusSession(f, focusReq()); err != nil {
		t.Fatal(err)
	}
	// The most recently active client is ttys002, in "play".
	if !reflect.DeepEqual(f.requested, [][2]string{{"%6", "%12"}}) || !reflect.DeepEqual(f.revealed, [][2]string{{"/dev/ttys002", "%6"}}) {
		t.Errorf("requested %v revealed %v", f.requested, f.revealed)
	}
	if len(f.floated)+len(f.popups) != 0 {
		t.Error("must not open another dashboard")
	}
}

func TestFocusPicksAnyInstanceAndSkipsDeadOnes(t *testing.T) {
	f := &fakeFocuser{clients: twoClients, instances: []tmux.Instance{
		{Pane: "%5", Session: "gone", PID: 1, Marked: true}, {Pane: "%6", Session: "other", PID: 2, Marked: true}}}
	r := focusReq()
	r.client = "/dev/ttys002"
	r.alive = func(pid int) bool { return pid == 2 }
	if err := focusSession(f, r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.requested, [][2]string{{"%6", "%12"}}) {
		t.Errorf("requested %v, want the live instance %%6", f.requested)
	}
}

func TestFocusOpensDashboardWithSelectWhenNoInstance(t *testing.T) {
	f := &fakeFocuser{clients: twoClients, instances: []tmux.Instance{{Pane: "%5", Session: "work", PID: 1, Marked: true}}}
	f.info = tmux.ServerInfo{Version: "3.8", WindowWidth: 200, WindowHeight: 50}
	r := focusReq()
	r.alive = func(int) bool { return false }
	r.client = "/dev/ttys001"
	if err := focusSession(f, r); err != nil {
		t.Fatal(err)
	}
	if len(f.requested) != 0 || len(f.floated) != 1 {
		t.Fatalf("requested %v floated %d", f.requested, len(f.floated))
	}
	o := f.floated[0]
	if o.Command != "'/bin/naviclaude' --select '%12'" || o.Target.Client != "/dev/ttys001" || o.Target.Pane != "%1" {
		t.Errorf("floated %#v", o)
	}
}

func TestFocusOpensDashboardWhenRevealFails(t *testing.T) {
	f := &fakeFocuser{clients: twoClients, revealErr: errors.New("gone"),
		instances: []tmux.Instance{{Pane: "%6", Session: "play", PID: 2, Marked: true}}}
	f.info = tmux.ServerInfo{Version: "3.7"}
	if err := focusSession(f, focusReq()); err != nil {
		t.Fatal(err)
	}
	if len(f.popups) != 1 || f.popups[0].Command != "'/bin/naviclaude' --select '%12'" {
		t.Errorf("popups %#v", f.popups)
	}
}

func TestFocusWithoutClientErrors(t *testing.T) {
	f := &fakeFocuser{}
	if err := focusSession(f, focusReq()); err == nil {
		t.Error("want an error with no attached client")
	}
	f = &fakeFocuser{clients: twoClients}
	r := focusReq()
	r.client = "/dev/nope"
	if err := focusSession(f, r); err == nil {
		t.Error("want an error for an unknown -c client")
	}
}

func TestFocusFindsUnmarkedDashboardButPrefersMarked(t *testing.T) {
	// An older build doesn't mark its pane; it is still found by its command.
	f := &fakeFocuser{clients: twoClients, instances: []tmux.Instance{{Pane: "%8", Session: "play"}}}
	if err := focusSession(f, focusReq()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.revealed, [][2]string{{"/dev/ttys002", "%8"}}) || len(f.floated)+len(f.popups) != 0 {
		t.Errorf("revealed %v, opened %d; want the unmarked dashboard shown, none opened", f.revealed, len(f.floated)+len(f.popups))
	}

	f = &fakeFocuser{clients: twoClients, instances: []tmux.Instance{
		{Pane: "%8", Session: "play"}, {Pane: "%9", Session: "work", PID: 3, Marked: true}}}
	if err := focusSession(f, focusReq()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.revealed, [][2]string{{"/dev/ttys002", "%9"}}) {
		t.Errorf("revealed %v, want the marked dashboard %%9 over the unmarked one", f.revealed)
	}
}
