package main

import (
	"errors"
	"testing"

	"github.com/thbits/naviClaude/internal/tmux"
)

type fakeLauncher struct {
	info      tmux.ServerInfo
	ours      bool
	floatErr  error
	floated   []tmux.FloatingPaneOptions
	popups    []tmux.DisplayPopupOptions
	notifyMsg string
}

func (f *fakeLauncher) QueryServer(tmux.LaunchTarget) (tmux.ServerInfo, error) { return f.info, nil }
func (f *fakeLauncher) IsLauncherPane(string) bool                             { return f.ours }
func (f *fakeLauncher) OpenFloatingPane(o tmux.FloatingPaneOptions) (string, error) {
	f.floated = append(f.floated, o)
	return "%9", f.floatErr
}
func (f *fakeLauncher) DisplayPopup(o tmux.DisplayPopupOptions) error {
	f.popups = append(f.popups, o)
	return nil
}
func (f *fakeLauncher) Notify(_ tmux.LaunchTarget, msg string) { f.notifyMsg = msg }

func testRequest() openRequest {
	return openRequest{
		target:    tmux.LaunchTarget{Client: "/dev/ttys001", Pane: "%3"},
		command:   "'/bin/naviclaude'",
		widthPct:  85,
		heightPct: 80,
	}
}

func TestOpenDashboardFloatsOn38(t *testing.T) {
	for _, v := range []string{"3.8", "next-3.9"} {
		f := &fakeLauncher{info: tmux.ServerInfo{Version: v, WindowWidth: 200, WindowHeight: 50}}
		if err := openDashboard(f, testRequest()); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
		if len(f.floated) != 1 || len(f.popups) != 0 {
			t.Fatalf("%s: floated=%d popups=%d, want a floating pane only", v, len(f.floated), len(f.popups))
		}
		o := f.floated[0]
		if o.Target.Pane != "%3" || o.WidthPercent != 85 || o.HeightPercent != 80 ||
			o.WindowWidth != 200 || o.WindowHeight != 50 || o.Command != "'/bin/naviclaude'" {
			t.Errorf("%s: unexpected options %+v", v, o)
		}
	}
}

func TestOpenDashboardPopupOnOlderTmux(t *testing.T) {
	for _, v := range []string{"3.7c", "3.4", "openbsd-7.6"} {
		f := &fakeLauncher{info: tmux.ServerInfo{Version: v, WindowWidth: 200, WindowHeight: 50}}
		if err := openDashboard(f, testRequest()); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
		if len(f.floated) != 0 || len(f.popups) != 1 {
			t.Fatalf("%s: floated=%d popups=%d, want a popup only", v, len(f.floated), len(f.popups))
		}
		p := f.popups[0]
		if p.Width != "85%" || p.Height != "80%" || p.Target.Client != "/dev/ttys001" || p.Command != "'/bin/naviclaude'" || p.Version != v {
			t.Errorf("%s: unexpected popup %+v", v, p)
		}
	}
}

func TestOpenDashboardForcePopup(t *testing.T) {
	f := &fakeLauncher{info: tmux.ServerInfo{Version: "3.8"}}
	r := testRequest()
	r.forcePopup = true
	if err := openDashboard(f, r); err != nil || len(f.floated) != 0 || len(f.popups) != 1 {
		t.Fatalf("err=%v floated=%d popups=%d", err, len(f.floated), len(f.popups))
	}
	if f.popups[0].Version != "3.8" {
		t.Errorf("popup Version = %q, want the server's 3.8", f.popups[0].Version)
	}
}

func TestOpenDashboardFallsBackWhenNewPaneFails(t *testing.T) {
	f := &fakeLauncher{info: tmux.ServerInfo{Version: "3.8"}, floatErr: errors.New("invalid width")}
	if err := openDashboard(f, testRequest()); err != nil || len(f.popups) != 1 {
		t.Fatalf("err=%v popups=%d, want popup fallback", err, len(f.popups))
	}
}

func TestOpenDashboardExistingModal(t *testing.T) {
	f := &fakeLauncher{info: tmux.ServerInfo{Version: "3.8", ModalPane: "%5"}, ours: true}
	if err := openDashboard(f, testRequest()); err != nil || len(f.floated)+len(f.popups) != 0 {
		t.Fatalf("own modal open: err=%v floated=%d popups=%d, want a no-op", err, len(f.floated), len(f.popups))
	}
	f = &fakeLauncher{info: tmux.ServerInfo{Version: "3.8", ModalPane: "%5"}}
	if err := openDashboard(f, testRequest()); !errors.Is(err, errOtherModal) || len(f.floated)+len(f.popups) != 0 {
		t.Fatalf("foreign modal open: err=%v floated=%d popups=%d", err, len(f.floated), len(f.popups))
	}
}
