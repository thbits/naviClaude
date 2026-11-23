package attention

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/harness/claude"
)

// writeLiveSession registers this test process as an interactive claude for sessionID.
func writeLiveSession(t *testing.T, sessionID, entrypoint string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pid := strconv.Itoa(os.Getpid())
	body := `{"pid":` + pid + `,"sessionId":"` + sessionID + `","kind":"interactive","entrypoint":"` + entrypoint + `","status":"idle"}`
	if err := os.WriteFile(filepath.Join(dir, pid+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHeadlessEntrypointFromEnv(t *testing.T) {
	writeLiveSession(t, "live", "sdk-cli")
	// "print-run" has no live status file, as a short `claude -p` child.
	for ep, want := range map[string]bool{"cli": false, "": false, "sdk-cli": true, "sdk-ts": true, "claude-vscode": false} {
		t.Setenv("CLAUDE_CODE_ENTRYPOINT", ep)
		if got := headlessHook("print-run"); got != want {
			t.Errorf("entrypoint %q: headless = %v, want %v", ep, got, want)
		}
	}
}

// An interactive claude that inherited CLAUDE_CODE_ENTRYPOINT=sdk-cli (e.g. a tmux
// server first started from an SDK run) must keep its hooks.
func TestInheritedSDKEntrypointInteractiveHookCounts(t *testing.T) {
	writeLiveSession(t, "live", "sdk-cli")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
	if headlessHook("live") {
		t.Error("hooks of a live interactive session were ignored")
	}
}

// A `claude -p` run from a busy session's Bash tool fires its own Stop with the
// parent's TMUX_PANE; it must not mark the parent done mid-turn.
func TestHeadlessChildStopDoesNotMarkParentDone(t *testing.T) {
	te := newTestEngine(func() claude.NativeState { return claude.NativeState{Status: "idle", UpdatedAt: 3} })
	te.nested = true
	te.fake.focused = false
	te.fake.panes["%1"].opts[optBusy] = "parent-turn"
	te.syncWindow("@1")

	te.Raise(KindDone, "%1", "child")
	te.Busy("%1", "child")
	te.End("%1")

	if len(te.played) != 0 {
		t.Errorf("headless child rang %v", te.played)
	}
	if a, b := te.fake.panes["%1"].opts[optAttn], te.fake.panes["%1"].opts[optBusy]; a != "" || b != "parent-turn" {
		t.Errorf("parent pane attn=%q busy=%q, want no attention and its own turn kept", a, b)
	}
	if got := te.fake.windowName(); got != markedName("cc", config.DefaultConfig().Notifications.BusyMarker) {
		t.Errorf("window = %q, want the parent's busy marker", got)
	}
}

// A prompt that never starts a turn (e.g. a local slash command like /clear)
// drops the busy marker after the grace period without marking anything done.
func TestBusyWithoutTurnNeverMarksDone(t *testing.T) {
	te := newTestEngine(func() claude.NativeState { return claude.NativeState{Status: "idle"} })
	te.fake.focused = false
	te.Busy("%1", "sid")
	if a, b := te.fake.panes["%1"].opts[optAttn], te.fake.panes["%1"].opts[optBusy]; a != "" || b != "" {
		t.Errorf("after an idle-only prompt attn=%q busy=%q, want both empty", a, b)
	}
	if len(te.played) != 0 || te.fake.windowName() != "cc" {
		t.Errorf("idle-only prompt rang %v / window %q", te.played, te.fake.windowName())
	}
}

// SessionEnd (sent for /clear, /resume switches and exit) drops a done marker.
func TestSessionEndClearsDone(t *testing.T) {
	te := newTestEngine(func() claude.NativeState { return claude.NativeState{Status: "idle", UpdatedAt: 3} })
	te.fake.focused = false
	te.MarkDone("%1")
	te.End("%1")
	if a := te.fake.panes["%1"].opts[optAttn]; a != "" || te.fake.windowName() != "cc" {
		t.Errorf("after SessionEnd attn=%q window=%q, want cleared", a, te.fake.windowName())
	}
}
