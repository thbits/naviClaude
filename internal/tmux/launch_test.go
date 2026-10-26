package tmux

import (
	"reflect"
	"testing"
)

func TestSupportsFloatingPanes(t *testing.T) {
	cases := map[string]bool{
		"3.8":         true,
		"3.8a":        true,
		"3.8-rc2":     true,
		"3.9":         true,
		"3.10":        true,
		"4.0":         true,
		"next-3.9":    true,
		"next-3.8":    true,
		" 3.8\n":      true,
		"3.7c":        false,
		"3.7":         false,
		"3.4":         false,
		"2.9a":        false,
		"next-3.7":    false,
		"openbsd-7.6": false,
		"openbsd-7.5": false,
		"openbsd-8.0": false,
		"master":      false,
		"":            false,
	}
	for v, want := range cases {
		if got := SupportsFloatingPanes(v); got != want {
			t.Errorf("SupportsFloatingPanes(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestParseVersionStringNextAndRC(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
	}{
		{"3.4", 3, 4}, {"3.7c", 3, 7}, {"next-3.9", 3, 9}, {"3.8-rc2", 3, 8}, {"3", 3, 0},
	}
	for _, c := range cases {
		maj, min, err := parseVersionString(c.in)
		if err != nil || maj != c.major || min != c.minor {
			t.Errorf("parseVersionString(%q) = %d.%d, %v; want %d.%d", c.in, maj, min, err, c.major, c.minor)
		}
	}
}

func TestParseServerInfo(t *testing.T) {
	got, err := parseServerInfo("3.8|~nc~|200|~nc~|49|~nc~|%7\n")
	if err != nil {
		t.Fatal(err)
	}
	want := ServerInfo{Version: "3.8", WindowWidth: 200, WindowHeight: 49, ModalPane: "%7"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// 3.7c expands the unknown #{window_modal_pane} to "".
	if got, err := parseServerInfo("3.7c|~nc~|80|~nc~|24|~nc~|\n"); err != nil || got.ModalPane != "" || got.WindowWidth != 80 {
		t.Errorf("3.7c output: got %+v, %v", got, err)
	}
	for _, bad := range []string{"", "3.8", "3.8|~nc~|x|~nc~|24|~nc~|"} {
		if _, err := parseServerInfo(bad); err == nil {
			t.Errorf("parseServerInfo(%q): want error", bad)
		}
	}
}

func TestFloatingPaneArgs(t *testing.T) {
	got := floatingPaneArgs(FloatingPaneOptions{
		Target:        LaunchTarget{Client: "/dev/ttys001", Pane: "%3"},
		WidthPercent:  85,
		HeightPercent: 85,
		WindowWidth:   200,
		WindowHeight:  50,
		Title:         "naviClaude",
		Command:       "'/usr/local/bin/naviclaude'",
	})
	// 85% of 200x50 is 170x42, centred at (15,4). new-pane takes no client.
	want := []string{"new-pane", "-O", "-P", "-F", "#{pane_id}", "-t", "%3",
		"-x", "170", "-y", "42", "-X", "15", "-Y", "4", "-T", "naviClaude",
		"'/usr/local/bin/naviclaude'",
		";", "set-option", "-pq", "remain-on-exit", "off",
		";", "set-option", "-pq", "@naviclaude_float", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("floatingPaneArgs:\n got %q\nwant %q", got, want)
	}
	for _, flag := range []string{"-C", "-D", "-K", "-W"} {
		for _, a := range got {
			if a == flag {
				t.Errorf("floatingPaneArgs must not pass %s", flag)
			}
		}
	}
}

func TestFloatingPaneArgsDefaultsAndBadPercent(t *testing.T) {
	got := floatingPaneArgs(FloatingPaneOptions{WidthPercent: 0, HeightPercent: 150, WindowWidth: 81, WindowHeight: 24, Command: "x"})
	want := []string{"new-pane", "-O", "-P", "-F", "#{pane_id}",
		"-x", "81", "-y", "24", "-X", "0", "-Y", "0", "x",
		";", "set-option", "-pq", "remain-on-exit", "off",
		";", "set-option", "-pq", "@naviclaude_float", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestDisplayPopupArgs(t *testing.T) {
	got := displayPopupArgs(DisplayPopupOptions{
		Target:  LaunchTarget{Client: "/dev/ttys001", Pane: "%3"},
		Width:   "85%",
		Height:  "80%",
		Command: "'/bin/naviclaude'",
	})
	want := []string{"display-popup", "-E", "-c", "/dev/ttys001", "-t", "%3",
		"-w", "85%", "-h", "80%", "-x", "C", "-y", "C", "'/bin/naviclaude'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("displayPopupArgs:\n got %q\nwant %q", got, want)
	}
	if got := displayPopupArgs(DisplayPopupOptions{Command: "x"}); !reflect.DeepEqual(got, []string{"display-popup", "-E", "-x", "C", "-y", "C", "x"}) {
		t.Errorf("no target: got %q", got)
	}
}

func TestDisplayPopupArgsStyleByVersion(t *testing.T) {
	styled := []string{"display-popup", "-E", "-x", "C", "-y", "C",
		"-s", "fg=terminal,bg=terminal", "-S", "fg=terminal,bg=terminal", "x"}
	plain := []string{"display-popup", "-E", "-x", "C", "-y", "C", "x"}
	cases := map[string][]string{
		"3.8": styled, "3.7c": styled, "3.3": styled, "next-3.9": styled,
		"3.2a": plain, "": plain, "openbsd-7.6": plain,
	}
	for v, want := range cases {
		if got := displayPopupArgs(DisplayPopupOptions{Command: "x", Version: v}); !reflect.DeepEqual(got, want) {
			t.Errorf("version %q:\n got %q\nwant %q", v, got, want)
		}
	}
}

func TestSupportsPopupStyle(t *testing.T) {
	cases := map[string]bool{"3.3": true, "3.3a": true, "3.8": true, "4.0": true, "3.2a": false, "2.9": false, "": false, "master": false}
	for v, want := range cases {
		if got := SupportsPopupStyle(v); got != want {
			t.Errorf("SupportsPopupStyle(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/naviclaude":   "'/usr/bin/naviclaude'",
		"/Users/a b/naviclaude": "'/Users/a b/naviclaude'",
		"/tmp/it's/naviclaude":  `'/tmp/it'\''s/naviclaude'`,
		"/tmp/$HOME;`x`/navi":   "'/tmp/$HOME;`x`/navi'",
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

// The set-options must follow new-pane in the same invocation and carry no -t,
// so they resolve to the new pane, the sequence's current target.
func TestFloatingPaneArgsSetsOptionsOnNewPane(t *testing.T) {
	got := floatingPaneArgs(FloatingPaneOptions{Target: LaunchTarget{Pane: "%3"}, WindowWidth: 80, WindowHeight: 24, Command: "x"})
	seps := 0
	for i, a := range got {
		if a == ";" {
			seps++
			if i+1 >= len(got) || got[i+1] != "set-option" {
				t.Errorf("command after ; = %q, want set-option", got[i+1:])
			}
		}
		if a == "-t" && seps > 0 {
			t.Errorf("set-option must not pass -t: %q", got)
		}
	}
	if seps != 2 {
		t.Errorf("got %d chained commands, want 2: %q", seps, got)
	}
}

func TestNotifyArgsEscapesFormats(t *testing.T) {
	got := notifyArgs(LaunchTarget{Client: "/dev/ttys001", Pane: "%3"}, "naviclaude: exec /tmp/#(touch x)/#{pane_id}: not found")
	want := []string{"display-message", "-c", "/dev/ttys001", "-t", "%3",
		"naviclaude: exec /tmp/##(touch x)/##{pane_id}: not found"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notifyArgs:\n got %q\nwant %q", got, want)
	}
	if got := notifyArgs(LaunchTarget{}, "50% done ##"); got[len(got)-1] != "50% done ####" {
		t.Errorf("notifyArgs without target = %q", got)
	}
}
