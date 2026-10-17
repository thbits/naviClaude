package attention

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/notify"
)

func TestClassify(t *testing.T) {
	bg := []json.RawMessage{json.RawMessage(`{"id":"x"}`)}
	cases := []struct {
		p    Payload
		want Kind
	}{
		{Payload{HookEventName: "PermissionRequest"}, KindAttention},
		{Payload{HookEventName: "Notification", NotificationType: "permission_prompt"}, KindAttention},
		{Payload{HookEventName: "Notification", NotificationType: "elicitation_dialog"}, KindAttention},
		{Payload{HookEventName: "Notification", NotificationType: "idle_prompt"}, KindIgnore},
		{Payload{HookEventName: "Notification", NotificationType: "agent_completed"}, KindIgnore},
		{Payload{HookEventName: "PreToolUse", ToolName: "AskUserQuestion"}, KindAttention},
		{Payload{HookEventName: "PreToolUse", ToolName: "Bash"}, KindIgnore},
		{Payload{HookEventName: "Stop"}, KindDone},
		{Payload{HookEventName: "Stop", BackgroundTasks: bg}, KindIgnore},
		{Payload{HookEventName: "StopFailure"}, KindDone},
		{Payload{HookEventName: "UserPromptSubmit"}, KindBusy},
		{Payload{HookEventName: "SessionEnd"}, KindClear},
		{Payload{HookEventName: "SubagentStop"}, KindIgnore},
	}
	for _, c := range cases {
		if got := Classify(c.p); got != c.want {
			t.Errorf("Classify(%+v) = %q, want %q", c.p, got, c.want)
		}
	}
}

const settingsFixture = `{
  "env": {
    "A": "1"
  },
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "npx -y skills update >/dev/null 2>&1 || true",
            "async": true
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "say done"
          }
        ]
      }
    ]
  },
  "zeta": "ünïcode <&>"
}
`

func TestInstallUninstallRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(real, []byte(settingsFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	exe := "/opt/navi claude/naviclaude"
	if err := InstallHooks(link, exe); err != nil {
		t.Fatal(err)
	}
	// Installing twice must not duplicate entries.
	if err := InstallHooks(link, exe); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("settings symlink was replaced by a regular file")
	}
	data, _ := os.ReadFile(real)
	got := string(data)
	if n := strings.Count(got, hookMarker); n != len(hookEvents) {
		t.Errorf("want %d naviclaude hooks, got %d:\n%s", len(hookEvents), n, got)
	}
	for _, keep := range []string{`>/dev/null 2>&1`, `"say done"`, `"ünïcode <&>"`} {
		if !strings.Contains(got, keep) {
			t.Errorf("lost or re-escaped %q:\n%s", keep, got)
		}
	}
	if strings.Index(got, `"env"`) > strings.Index(got, `"zeta"`) {
		t.Error("top-level key order changed")
	}
	events, cmd, err := InstalledHooks(link)
	if err != nil || len(events) != len(hookEvents) || cmd != HookCommand(exe) {
		t.Errorf("InstalledHooks = %v %q %v", events, cmd, err)
	}

	if err := UninstallHooks(link); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(real)
	if string(data) != settingsFixture {
		t.Errorf("uninstall did not restore the original file:\n%s", data)
	}
}

func TestUninstallWithoutHooksLeavesFileAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := UninstallHooks(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("uninstall created %s (err %v)", path, err)
	}

	orig := "{\n    \"model\": \"opus\"\n}\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UninstallHooks(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != orig {
		t.Errorf("uninstall rewrote a file it had nothing to remove from:\n%s", data)
	}
}

// fakeTmux models the panes and windows the engine touches.
type fakeTmux struct {
	mu       sync.Mutex
	panes    map[string]*fakePane
	windows  map[string]*fakeWindow
	focused  bool
	lastRing int64
}

type fakePane struct {
	window   string
	opts     map[string]string
	onScreen bool
}

type fakeWindow struct {
	name      string
	opts      map[string]string
	localAuto string
}

func newFakeTmux() *fakeTmux {
	return &fakeTmux{
		panes:   map[string]*fakePane{"%1": {window: "@1", opts: map[string]string{}}, "%2": {window: "@1", opts: map[string]string{}}},
		windows: map[string]*fakeWindow{"@1": {name: "cc", opts: map[string]string{}}},
		focused: true,
	}
}

func (f *fakeTmux) Pane(p string) (paneState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.panes[p]
	if !ok {
		return paneState{}, errors.New("no pane")
	}
	su, _ := strconv.ParseInt(fp.opts[optAttnSU], 10, 64)
	return paneState{Window: fp.window, Attn: fp.opts[optAttn], AttnSU: su, Token: fp.opts[optToken], OnScreen: fp.onScreen, Session: "s", Busy: fp.opts[optBusy]}, nil
}

func setOpts(m map[string]string, kv map[string]string) {
	for k, v := range kv {
		if v == "" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
}

func (f *fakeTmux) SetPaneOptions(p string, kv map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	setOpts(f.panes[p].opts, kv)
	return nil
}

func (f *fakeTmux) WindowPanes(w string) ([]paneMarks, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []paneMarks
	for _, p := range f.panes {
		if p.window == w {
			out = append(out, paneMarks{Attn: p.opts[optAttn], Busy: p.opts[optBusy]})
		}
	}
	return out, nil
}

func (f *fakeTmux) Window(w string) (windowState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fw := f.windows[w]
	return windowState{Name: fw.name, Marker: fw.opts[optMarker], OrigName: fw.opts[optOrigName], OrigAuto: fw.opts[optOrigAuto]}, nil
}

func (f *fakeTmux) LocalAutoRename(w string) string { return f.windows[w].localAuto }

func (f *fakeTmux) RenameWindow(w, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.windows[w].name = name
	f.windows[w].localAuto = "off"
	return nil
}

func (f *fakeTmux) SetWindowOptions(w string, kv map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fw := f.windows[w]
	if v, ok := kv["automatic-rename"]; ok {
		fw.localAuto = v
		delete(kv, "automatic-rename")
	}
	setOpts(fw.opts, kv)
	return nil
}

func (f *fakeTmux) SessionFocused(string) bool { return f.focused }
func (f *fakeTmux) LastRing() int64            { return f.lastRing }
func (f *fakeTmux) SetLastRing(ms int64)       { f.lastRing = ms }

func (f *fakeTmux) windowName() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.windows["@1"].name
}

// testEngine wires an engine to a fake tmux, a scripted native status and a
// fake clock that advances on every sleep.
type testEngine struct {
	*engine
	fake   *fakeTmux
	status func() claude.NativeState
	played []notify.Sound
	clock  time.Time
}

func newTestEngine(status func() claude.NativeState) *testEngine {
	te := &testEngine{fake: newFakeTmux(), status: status, clock: time.Unix(1000, 0)}
	cfg := config.DefaultConfig().Notifications
	te.engine = &engine{
		tmux:    te.fake,
		cfg:     cfg,
		native:  func(string) (claude.NativeState, bool) { return te.status(), true },
		play:    func(s notify.Sound, _ string, _ float64) error { te.played = append(te.played, s); return nil },
		now:     func() time.Time { return te.clock },
		sleep:   func(d time.Duration) { te.clock = te.clock.Add(d) },
		lock:    func() func() { return func() {} },
		logf:    func(string, ...any) {},
		poll:    500 * time.Millisecond,
		maxLife: time.Minute,
	}
	return te
}

func TestRaiseWaitingMarksWindowThenClearsWhenAnswered(t *testing.T) {
	polls := 0
	var te *testEngine
	te = newTestEngine(func() claude.NativeState {
		polls++
		if polls == 2 {
			// Read 1 is the confirm; read 2 is the watcher's first poll.
			if got := te.fake.windowName(); got != markedName("cc", config.DefaultConfig().Notifications.WaitingMarker) {
				t.Errorf("window while waiting = %q, want %q", got, markedName("cc", config.DefaultConfig().Notifications.WaitingMarker))
			}
			return claude.NativeState{Status: "waiting", WaitingFor: "permission prompt", UpdatedAt: 7}
		}
		if polls < 4 {
			return claude.NativeState{Status: "waiting", WaitingFor: "permission prompt", UpdatedAt: 7}
		}
		return claude.NativeState{Status: "busy", UpdatedAt: 9}
	})
	te.fake.focused = false

	te.Raise(KindAttention, "%1", "sid")

	if len(te.played) != 1 || te.played[0] != notify.Request {
		t.Errorf("played %v, want [request]", te.played)
	}
	if got := te.fake.windowName(); got != "cc" {
		t.Errorf("window after answer = %q, want restored %q", got, "cc")
	}
	if a := te.fake.windows["@1"].localAuto; a != "" {
		t.Errorf("automatic-rename left as %q, want inherited", a)
	}
	if te.fake.panes["%1"].opts[optAttn] != "" {
		t.Error("attention not cleared")
	}
}

func TestRaiseSkipsWhenAutoApproved(t *testing.T) {
	te := newTestEngine(func() claude.NativeState { return claude.NativeState{Status: "busy"} })
	te.Raise(KindAttention, "%1", "sid")
	if len(te.played) != 0 || te.fake.windowName() != "cc" {
		t.Errorf("auto-approved prompt rang %v / window %q", te.played, te.fake.windowName())
	}
}

func TestRaiseWhileFocusedRingsOnceWithoutMarker(t *testing.T) {
	te := newTestEngine(func() claude.NativeState {
		return claude.NativeState{Status: "waiting", UpdatedAt: 7}
	})
	te.fake.panes["%1"].onScreen = true
	te.Raise(KindAttention, "%1", "sid")
	te.clock = te.clock.Add(10 * time.Second) // past the cooldown
	te.Raise(KindAttention, "%1", "sid")      // the Notification hook for the same prompt
	if len(te.played) != 1 {
		t.Errorf("played %v, want one request", te.played)
	}
	if te.fake.windowName() != "cc" {
		t.Errorf("focused pane got a marker: %q", te.fake.windowName())
	}
}

func TestDoneSilentWhenFocused(t *testing.T) {
	te := newTestEngine(func() claude.NativeState { return claude.NativeState{Status: "idle", UpdatedAt: 3} })
	te.fake.panes["%1"].onScreen = true
	te.Raise(KindDone, "%1", "sid")
	if len(te.played) != 0 {
		t.Errorf("done rang while focused: %v", te.played)
	}
}

func TestDoneInBackgroundClearsOnVisit(t *testing.T) {
	var te *testEngine
	polls := 0
	te = newTestEngine(func() claude.NativeState {
		polls++
		if polls == 3 {
			if got := te.fake.windowName(); got != markedName("cc", config.DefaultConfig().Notifications.DoneMarker) {
				t.Errorf("window while done = %q", got)
			}
			te.fake.panes["%1"].onScreen = true // user switches to the pane
			te.fake.focused = true
		}
		return claude.NativeState{Status: "idle", UpdatedAt: 3}
	})
	te.fake.focused = false
	te.Raise(KindDone, "%1", "sid")
	if len(te.played) != 1 || te.played[0] != notify.Done {
		t.Errorf("played %v, want [done]", te.played)
	}
	if got := te.fake.windowName(); got != "cc" {
		t.Errorf("window after visit = %q, want %q", got, "cc")
	}
	if polls > 5 {
		t.Errorf("watcher kept polling after the visit (%d polls)", polls)
	}
}

func TestWaitingBeatsDoneAcrossPanes(t *testing.T) {
	te := newTestEngine(nil)
	te.fake.panes["%1"].opts[optAttn] = Done
	te.fake.panes["%2"].opts[optAttn] = Waiting
	te.syncWindow("@1")
	if got := te.fake.windowName(); got != markedName("cc", config.DefaultConfig().Notifications.WaitingMarker) {
		t.Errorf("got %q", got)
	}
	te.fake.panes["%2"].opts[optAttn] = ""
	te.syncWindow("@1")
	if got := te.fake.windowName(); got != markedName("cc", config.DefaultConfig().Notifications.DoneMarker) {
		t.Errorf("got %q", got)
	}
	te.Clear("%1")
	if got := te.fake.windowName(); got != "cc" {
		t.Errorf("got %q", got)
	}
}

func TestUserRenameWhileMarkedIsKept(t *testing.T) {
	te := newTestEngine(nil)
	te.fake.panes["%1"].opts[optAttn] = Waiting
	te.syncWindow("@1")
	te.fake.windows["@1"].name = "mine"
	te.Clear("%1")
	if got := te.fake.windowName(); got != "mine" {
		t.Errorf("user rename overwritten: %q", got)
	}
}

func TestCooldownAcrossSessions(t *testing.T) {
	te := newTestEngine(func() claude.NativeState { return claude.NativeState{Status: "waiting", UpdatedAt: 7} })
	te.fake.panes["%1"].onScreen, te.fake.panes["%2"].onScreen = true, true
	te.Raise(KindAttention, "%1", "a")
	te.Raise(KindAttention, "%2", "b")
	if len(te.played) != 1 {
		t.Errorf("cooldown let %d sounds through", len(te.played))
	}
}

func TestClosedPaneDropsWindowMarker(t *testing.T) {
	var te *testEngine
	polls := 0
	te = newTestEngine(func() claude.NativeState {
		polls++
		if polls == 2 {
			te.fake.mu.Lock()
			delete(te.fake.panes, "%1") // pane killed without SessionEnd
			te.fake.mu.Unlock()
		}
		return claude.NativeState{Status: "waiting", UpdatedAt: 7}
	})
	te.fake.focused = false
	te.Raise(KindAttention, "%1", "sid")
	if got := te.fake.windowName(); got != "cc" {
		t.Errorf("window after pane closed = %q, want %q", got, "cc")
	}
}

func TestLegacyPrefixMarkerIsRestored(t *testing.T) {
	te := newTestEngine(nil)
	w := te.fake.windows["@1"]
	w.name = "🔔 cc"
	w.opts[optMarker], w.opts[optOrigName] = "🔔", "cc"
	te.Clear("%1")
	if got := te.fake.windowName(); got != "cc" {
		t.Errorf("legacy marker not restored: %q", got)
	}
}

func TestBusyMarksWindowUntilTurnEnds(t *testing.T) {
	marks := config.DefaultConfig().Notifications
	status := []string{"idle", "busy", "waiting", "busy", "idle"} // stale idle, then the turn
	polls := 0
	var te *testEngine
	te = newTestEngine(func() claude.NativeState {
		want := marks.BusyMarker
		if status[polls] == "waiting" {
			te.fake.panes["%1"].opts[optAttn] = Waiting
			te.syncWindow("@1")
			want = marks.WaitingMarker
		} else if polls > 0 && status[polls-1] == "waiting" {
			te.Clear("%1") // the waiting watcher resolves once answered
		}
		if got := te.fake.windowName(); got != markedName("cc", want) {
			t.Errorf("poll %d (%s): window = %q, want %q marker", polls, status[polls], got, want)
		}
		st := claude.NativeState{Status: status[polls]}
		polls++
		return st
	})
	te.Busy("%1", "sid")
	if got := te.fake.windowName(); got != "cc" {
		t.Errorf("window after turn = %q, want %q", got, "cc")
	}
	if polls != len(status) {
		t.Errorf("polled %d times, want %d", polls, len(status))
	}
}

func TestDoneReplacesBusy(t *testing.T) {
	te := newTestEngine(func() claude.NativeState { return claude.NativeState{Status: "idle", UpdatedAt: 3} })
	te.fake.panes["%1"].opts[optBusy] = "tok"
	te.fake.panes["%1"].onScreen = true
	te.Raise(KindDone, "%1", "sid")
	if b := te.fake.panes["%1"].opts[optBusy]; b != "" {
		t.Errorf("busy left set: %q", b)
	}
	if got := te.fake.windowName(); got != "cc" {
		t.Errorf("window = %q, want %q", got, "cc")
	}
}

func TestPreviewClearKeepsBusy(t *testing.T) {
	te := newTestEngine(nil)
	te.fake.panes["%1"].opts[optBusy] = "tok"
	te.Clear("%1")
	if got := te.fake.windowName(); got != markedName("cc", config.DefaultConfig().Notifications.BusyMarker) {
		t.Errorf("window = %q, want busy marker", got)
	}
	te.End("%1")
	if got := te.fake.windowName(); got != "cc" {
		t.Errorf("window after SessionEnd = %q", got)
	}
}

func TestBusyMarkerNone(t *testing.T) {
	te := newTestEngine(nil)
	te.cfg.BusyMarker = "none"
	te.fake.panes["%1"].opts[optBusy] = "tok"
	te.syncWindow("@1")
	if got := te.fake.windowName(); got != "cc" {
		t.Errorf("window = %q, want no marker", got)
	}
}
