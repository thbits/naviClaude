package tmux

import (
	"os"
	"reflect"
	"strconv"
	"testing"
)

func TestParseClientsAndInstances(t *testing.T) {
	cs := parseClients("10" + FieldSep + "/dev/ttys001" + FieldSep + "my sess" + FieldSep + "%3\nbad line\n")
	want := []ClientInfo{{Name: "/dev/ttys001", Activity: 10, Session: "my sess", Pane: "%3"}}
	if !reflect.DeepEqual(cs, want) {
		t.Errorf("clients = %#v, want %#v", cs, want)
	}
	in := parseInstances("%9" + FieldSep + "b" + FieldSep + "42" + FieldSep + "naviclaude\n%2" + FieldSep + "a" + FieldSep + "7" + FieldSep + "naviclaude\n" +
		"%5" + FieldSep + "c" + FieldSep + "" + FieldSep + "zsh\n%7" + FieldSep + "d" + FieldSep + "" + FieldSep + "naviclaude\n")
	wantIn := []Instance{{Pane: "%2", Session: "a", PID: 7, Marked: true}, {Pane: "%7", Session: "d"}, {Pane: "%9", Session: "b", PID: 42, Marked: true}}
	if !reflect.DeepEqual(in, wantIn) {
		t.Errorf("instances = %#v, want %#v", in, wantIn)
	}
}

// The mark, the select request and the unmark round-trip on a real private server.
func TestInstanceMarkAndSelectOnRealTmux(t *testing.T) {
	tm, _ := privateTmux(t)
	tm("-f", "/dev/null", "new-session", "-d", "-s", "s1", "-x", "90", "-y", "30", "sleep 60")
	pane := tm("display-message", "-p", "-t", "s1:0.0", "#{pane_id}")
	c := &Client{}

	if err := c.MarkInstance(pane, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	got, err := c.ListInstances()
	if err != nil || len(got) != 1 || got[0].Pane != pane || got[0].Session != "s1" || got[0].PID != os.Getpid() {
		t.Fatalf("ListInstances = %#v, %v", got, err)
	}

	if v := c.TakeSelect(pane); v != "" {
		t.Errorf("TakeSelect before a request = %q", v)
	}
	if err := c.RequestSelect(pane, "%12"); err != nil {
		t.Fatal(err)
	}
	if v := c.TakeSelect(pane); v != "%12" {
		t.Errorf("TakeSelect = %q, want %%12", v)
	}
	if v := c.TakeSelect(pane); v != "" {
		t.Errorf("a request must be taken once, got %q again", v)
	}

	c.UnmarkInstance(pane, os.Getpid()+1) // someone else's mark stays
	if got, _ := c.ListInstances(); len(got) != 1 {
		t.Errorf("unmark with another pid removed the mark: %#v", got)
	}
	c.UnmarkInstance(pane, os.Getpid())
	if got, _ := c.ListInstances(); len(got) != 0 {
		t.Errorf("mark left after unmark: %#v (pid %s)", got, strconv.Itoa(os.Getpid()))
	}
}
