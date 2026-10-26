package tmux

import (
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseOptionLine(t *testing.T) {
	cases := []struct {
		line, name, value string
	}{
		{"window-size manual", "window-size", "manual"},
		{"@naviclaude_presize auto\n", optPreSize, "auto"},
		{"@naviclaude_presize ''", optPreSize, ""},
		{`@x "a b"`, "@x", "a b"},
		{"", "", ""},
	}
	for _, c := range cases {
		name, value := parseOptionLine(c.line)
		if name != c.name || value != c.value {
			t.Errorf("parseOptionLine(%q) = %q, %q; want %q, %q", c.line, name, value, c.name, c.value)
		}
	}
}

func TestChainSkipsEmptyCommands(t *testing.T) {
	got := chain(nil, []string{"a", "1"}, nil, []string{"b"}, nil)
	want := []string{"a", "1", ";", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("chain = %q, want %q", got, want)
	}
	if got := chain(nil, nil); got != nil {
		t.Errorf("chain of nothing = %q, want nil", got)
	}
}

func TestSaveArgs(t *testing.T) {
	if got := saveArgs("s:1", windowOptions{preSize: "largest"}); got != nil {
		t.Errorf("existing presize must be kept, got %q", got)
	}
	cases := map[string]string{"": "auto", "manual": "manual", "latest": "latest"}
	for size, pre := range cases {
		got := saveArgs("s:1", windowOptions{size: size})
		want := []string{"set-option", "-wq", "-t", "s:1", optPreSize, pre}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("saveArgs(size=%q) = %q, want %q", size, got, want)
		}
	}
}

func TestRestoreArgs(t *testing.T) {
	cases := []struct {
		pre  string
		want []string
	}{
		{"", []string{"resize-window", "-t", "s:1", "-x", "80", "-y", "24"}},
		{"auto", []string{
			"resize-window", "-A", "-t", "s:1", ";",
			"set-option", "-wqu", "-t", "s:1", "window-size", ";",
			"set-option", "-wqu", "-t", "s:1", optPreSize,
		}},
		{"largest", []string{
			"resize-window", "-t", "s:1", "-x", "80", "-y", "24", ";",
			"set-option", "-wq", "-t", "s:1", "window-size", "largest", ";",
			"set-option", "-wqu", "-t", "s:1", optPreSize,
		}},
	}
	for _, c := range cases {
		if got := restoreArgs("s:1", 80, 24, c.pre); !reflect.DeepEqual(got, c.want) {
			t.Errorf("restoreArgs(pre=%q) = %q, want %q", c.pre, got, c.want)
		}
	}
	// resize-window pins window-size to manual, so the reset must follow it.
	args := restoreArgs("s:1", 80, 24, "auto")
	if slices.Index(args, "resize-window") > slices.Index(args, "window-size") {
		t.Errorf("window-size reset precedes resize-window: %q", args)
	}
}

func TestPreviewReadArgs(t *testing.T) {
	got := previewReadArgs("s:2", "")
	want := []string{
		"display-message", "-p", "-t", "s:2", "#{window_width} #{window_height} #{window_id}", ";",
		"show-options", "-wq", "-t", "s:2", "window-size", ";",
		"show-options", "-wq", "-t", "s:2", optPreSize,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("previewReadArgs without prev = %q, want %q", got, want)
	}
	got = previewReadArgs("s:2", "s:1")
	want = append(want, ";", "display-message", "-p", "-t", "s:1", readMarker+"#{window_id}", ";",
		"show-options", "-wq", "-t", "s:1", optPreSize)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("previewReadArgs with prev = %q, want %q", got, want)
	}
}

func TestParsePreviewRead(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want previewRead
	}{
		{"inherited, no prev", "120 40\n", previewRead{w: 120, h: 40}},
		{"own size and stale presize",
			"120 40\nwindow-size manual\n@naviclaude_presize largest\n",
			previewRead{w: 120, h: 40, opts: windowOptions{size: "manual", preSize: "largest"}}},
		// Both windows print the same option name; only the marker tells them apart.
		{"prev presize after marker",
			"90 30\nwindow-size latest\n" + readMarker + "\n@naviclaude_presize auto\n",
			previewRead{w: 90, h: 30, opts: windowOptions{size: "latest"}, prevPre: "auto"}},
		{"prev gone or unset", "90 30\n" + readMarker + "\n", previewRead{w: 90, h: 30}},
		{"window ids", "90 30 @3\n" + readMarker + "@3\n", previewRead{w: 90, h: 30, winID: "@3", prevWinID: "@3"}},
		{"empty presize", "90 30\n@naviclaude_presize ''\n" + readMarker + "\n@naviclaude_presize ''\n",
			previewRead{w: 90, h: 30}},
	}
	for _, c := range cases {
		got := parsePreviewRead(c.out)
		if got.sizeErr != nil {
			t.Errorf("%s: unexpected error %v", c.name, got.sizeErr)
		}
		got.sizeErr = nil
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
	if r := parsePreviewRead(""); r.sizeErr == nil {
		t.Error("empty output must report a size error")
	}
}

// TestFitPreviewWindowGroupedSession runs a private tmux server: grouped sessions
// name one window twice, and moving between them must still restore it on leave.
func TestFitPreviewWindowGroupedSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "nvg") // t.TempDir is too long for a socket path
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", dir)
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", args...).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() { tm("kill-server") })
	tm("-f", "/dev/null", "new-session", "-d", "-s", "s", "-x", "80", "-y", "24")
	tm("new-session", "-d", "-s", "g", "-t", "s")
	state := func() string {
		return tm("display-message", "-p", "-t", "s:0", "#{window_width}x#{window_height}") +
			" [" + tm("show-options", "-wq", "-t", "s:0", "window-size") + "]" +
			" [" + tm("show-options", "-wq", "-t", "s:0", optPreSize) + "]"
	}

	c := &Client{}
	w, h, _ := c.FitPreviewWindow("s:0", 120, 40, "", 0, 0)
	w, h, _ = c.FitPreviewWindow("g:0", 120, 40, "s:0", w, h)
	if w != 80 || h != 24 {
		t.Errorf("recorded original size %dx%d, want 80x24", w, h)
	}
	c.RestoreWindowSize("g:0", w, h)
	if got := state(); got != "80x24 [] []" {
		t.Errorf("window after leave = %q, want restored 80x24 with no options", got)
	}
}
