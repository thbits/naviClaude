package tmux

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const testServerPID = 100

// Process tree: 100 tmux server; 200 pane shell -> 201 naviclaude; 300 floating pane
// shell -> 301 naviclaude; 400 popup job shell -> 401 naviclaude; 500 is naviclaude
// itself as a pane process; 900 -> 901 runs outside tmux.
var testParents = map[int]int{
	100: 1,
	200: 100, 201: 200,
	300: 100, 301: 300,
	400: 100, 401: 400,
	500: 100,
	900: 1, 901: 900,
}

var testPanePIDs = map[int]bool{200: true, 300: true, 500: true}

func testParentOf(pid int) (int, bool) {
	p, ok := testParents[pid]
	return p, ok
}

func TestIsPopupJob(t *testing.T) {
	cases := []struct {
		name string
		pid  int
		want bool
	}{
		{"popup job", 401, true},
		{"popup job's own shell", 400, true},
		{"pane", 201, false},
		{"floating pane", 301, false},
		{"pane process itself", 500, false},
		{"outside tmux", 901, false},
		{"not in snapshot", 12345, false},
	}
	for _, c := range cases {
		if got := isPopupJob(c.pid, testServerPID, testPanePIDs, testParentOf); got != c.want {
			t.Errorf("%s: isPopupJob(%d) = %v, want %v", c.name, c.pid, got, c.want)
		}
	}
	// A parent loop must not hang.
	loop := func(pid int) (int, bool) { return pid + 1, true }
	if isPopupJob(2, testServerPID+1000, nil, loop) {
		t.Error("runaway chain: want false")
	}
}

func TestParseTMUXEnv(t *testing.T) {
	cases := []struct {
		in       string
		pid, sid int
		ok       bool
	}{
		{"/private/tmp/tmux-501/default,44137,0", 44137, 0, true},
		{"/tmp/a,b/sock,77,12", 77, 12, true},
		{"/tmp/sock,77,-1", 0, 0, false},
		{"/tmp/sock,1,0", 0, 0, false},
		{"/tmp/sock,x,0", 0, 0, false},
		{"/tmp/sock", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		pid, sid, ok := parseTMUXEnv(c.in)
		if pid != c.pid || sid != c.sid || ok != c.ok {
			t.Errorf("parseTMUXEnv(%q) = %d, %d, %v; want %d, %d, %v", c.in, pid, sid, ok, c.pid, c.sid, c.ok)
		}
	}
}

func TestRestylePopupArgs(t *testing.T) {
	got := restylePopupArgs("/dev/ttys007")
	want := []string{"display-popup", "-c", "/dev/ttys007", "-w", "99999", "-h", "99999",
		"-s", "fg=terminal,bg=terminal", "-S", "fg=terminal,bg=terminal", "true"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restylePopupArgs:\n got %q\nwant %q", got, want)
	}
	// Each of these would change the open popup's behaviour or title.
	for _, flag := range []string{"-E", "-k", "-N", "-b", "-B", "-T", "-C", "-d", "-e"} {
		for _, a := range got {
			if a == flag {
				t.Errorf("restylePopupArgs must not pass %s", flag)
			}
		}
	}
}

// fakeTmux answers the commands restyleOwnPopup runs and records them.
type fakeTmux struct {
	version  string
	panes    string
	clients  string
	popupErr error
	calls    [][]string
}

func (f *fakeTmux) run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	switch args[0] {
	case "display-message":
		return f.version + "\n", nil
	case "list-panes":
		return f.panes, nil
	case "list-clients":
		return f.clients, nil
	case "display-popup":
		return "", f.popupErr
	}
	return "", errors.New("unexpected command " + strings.Join(args, " "))
}

func (f *fakeTmux) ran(cmd string) bool {
	for _, c := range f.calls {
		if c[0] == cmd {
			return true
		}
	}
	return false
}

func popupTestEnv(pid int) popupEnv {
	return popupEnv{tmux: "/tmp/tmux-501/default,100,3", pid: pid, parentOf: testParentOf}
}

func TestRestyleOwnPopupInPopup(t *testing.T) {
	f := &fakeTmux{version: "3.8", panes: "200\n300\n500\n", clients: "/dev/ttys007\n"}
	got := restyleOwnPopup(popupTestEnv(401), f.run)
	if !reflect.DeepEqual(got, restylePopupArgs("/dev/ttys007")) {
		t.Fatalf("got %q, want the restyle argv", got)
	}
	want := [][]string{
		{"display-message", "-p", "-t", "$3", "#{version}"},
		{"list-panes", "-a", "-F", "#{pane_pid}"},
		{"list-clients", "-t", "$3", "-F", "#{client_name}"},
		restylePopupArgs("/dev/ttys007"),
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls:\n got %q\nwant %q", f.calls, want)
	}
}

func TestRestyleOwnPopupSkips(t *testing.T) {
	inPane := popupTestEnv(201)
	inPane.tmuxPane = "%0"
	floating := popupTestEnv(301)
	floating.tmuxPane = "%4"
	unsetPane := popupTestEnv(201) // TMUX_PANE unset by hand inside a pane
	cases := []struct {
		name      string
		env       popupEnv
		f         *fakeTmux
		wantCalls bool // whether tmux should be queried at all
	}{
		{"outside tmux", popupEnv{pid: 901, parentOf: testParentOf}, &fakeTmux{version: "3.8"}, false},
		{"pane", inPane, &fakeTmux{version: "3.8"}, false},
		{"floating pane", floating, &fakeTmux{version: "3.8"}, false},
		{"pane with TMUX_PANE unset", unsetPane, &fakeTmux{version: "3.8", panes: "200 300 500", clients: "/dev/ttys007"}, true},
		{"tmux 3.7c", popupTestEnv(401), &fakeTmux{version: "3.7c", panes: "200", clients: "/dev/ttys007"}, true},
		{"unparseable version", popupTestEnv(401), &fakeTmux{version: "master", panes: "200", clients: "/dev/ttys007"}, true},
		{"two clients", popupTestEnv(401), &fakeTmux{version: "3.8", panes: "200", clients: "/dev/ttys007\n/dev/ttys008\n"}, true},
		{"no client", popupTestEnv(401), &fakeTmux{version: "3.8", panes: "200", clients: ""}, true},
		{"modify refused", popupTestEnv(401), &fakeTmux{version: "3.8", panes: "200", clients: "/dev/ttys007", popupErr: errors.New("height too large")}, true},
	}
	for _, c := range cases {
		if got := restyleOwnPopup(c.env, c.f.run); got != nil {
			t.Errorf("%s: got %q, want nil", c.name, got)
		}
		if (len(c.f.calls) > 0) != c.wantCalls {
			t.Errorf("%s: calls %q, want any=%v", c.name, c.f.calls, c.wantCalls)
		}
		if c.name != "modify refused" && c.f.ran("display-popup") {
			t.Errorf("%s: ran display-popup", c.name)
		}
	}
}
