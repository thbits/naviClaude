package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
)

// The detector only skips headless runs for harnesses that implement the filter.
var _ harness.HeadlessFilter = (*Harness)(nil)

// writeEntrypointMeta writes a live status file the way Claude 2.1.330 does,
// with the entrypoint it records for interactive ("cli") and print/SDK runs.
func writeEntrypointMeta(t *testing.T, home string, pid int, sessionID, tmux, entrypoint string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"pid":` + strconv.Itoa(pid) + `,"sessionId":"` + sessionID + `","tmux":"` + tmux +
		`","kind":"interactive","entrypoint":"` + entrypoint + `","status":"busy"}`
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeProc makes lookupProc report cmdLine for pid until the test ends.
func fakeProc(t *testing.T, pid int, cmdLine string) {
	t.Helper()
	procCache.Store(pid, procInfo{cmdLine: cmdLine, fetched: time.Now().Add(time.Hour)})
	t.Cleanup(func() { procCache.Delete(pid) })
}

func TestHeadlessFromEntrypoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := New(Options{})
	for pid, ep := range map[int]string{101: "cli", 102: "sdk-cli", 103: "sdk-ts", 104: "sdk-py", 105: "claude-vscode", 107: "sdk-cli"} {
		writeEntrypointMeta(t, home, pid, "s"+strconv.Itoa(pid), "", ep)
	}
	fakeProc(t, 101, "claude")
	fakeProc(t, 102, "claude -p summarise this")
	fakeProc(t, 103, "claude --output-format stream-json --verbose --input-format stream-json")
	fakeProc(t, 104, "node cli.js --print --output-format=json")
	fakeProc(t, 105, "claude")
	// An interactive claude started where CLAUDE_CODE_ENTRYPOINT=sdk-cli was inherited.
	fakeProc(t, 107, "claude --resume abc")
	want := map[int]bool{101: false, 102: true, 103: true, 104: true, 105: false, 106: false, 107: false}
	for pid, w := range want {
		if got := h.Headless(pid); got != w {
			t.Errorf("Headless(%d) = %v, want %v", pid, got, w)
		}
	}
}

func TestFallbackSkipsHeadlessChildInSamePane(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// A `claude -p` run from the session's Bash tool inherits its TMUX_PANE.
	writeEntrypointMeta(t, home, os.Getpid(), "child", "work:@3.%9", "sdk-cli")
	fakeProc(t, os.Getpid(), "claude -p run the linter")

	if got := extractSessionIDFallback(deadPID, "/w", "%9"); got == "child" {
		t.Errorf("pane %%9 took the headless child's session ID %q", got)
	}
}

// An interactive session with an inherited sdk-* entrypoint is still the pane's session.
func TestFallbackKeepsInteractiveWithInheritedSDKEntrypoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeEntrypointMeta(t, home, os.Getpid(), "own", "work:@3.%9", "sdk-cli")
	fakeProc(t, os.Getpid(), "claude")

	if got := extractSessionIDFallback(deadPID, "/w", "%9"); got != "own" {
		t.Errorf("pane %%9 session = %q, want own", got)
	}
	if New(Options{}).Headless(os.Getpid()) {
		t.Error("interactive claude with entrypoint sdk-cli was filtered as headless")
	}
}

func TestHeadlessSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeEntrypointMeta(t, home, os.Getpid(), "live", "work:@3.%9", "sdk-cli")
	fakeProc(t, os.Getpid(), "claude")
	if HeadlessSession("live") {
		t.Error("an interactive session's hooks would be ignored")
	}
	if !HeadlessSession("print-run") {
		t.Error("a session with no live status file must count as headless")
	}
	fakeProc(t, os.Getpid(), "claude -p x")
	if !HeadlessSession("live") {
		t.Error("a -p run's status file must count as headless")
	}
}

// /clear and /resume rewrite sessionId in the same <pid>.json; each scan's
// Enrich must pick up the new ID rather than keep the first one.
func TestEnrichFollowsInPlaceSessionSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := New(Options{})
	pid := os.Getpid()

	writeEntrypointMeta(t, home, pid, "before-clear", "work:@1.%1", "cli")
	s := &session.Session{PID: pid, CWD: "/w", TmuxPaneID: "%1"}
	h.Enrich(s)
	if s.ID != "before-clear" {
		t.Fatalf("ID = %q, want before-clear", s.ID)
	}

	writeEntrypointMeta(t, home, pid, "after-clear", "work:@1.%1", "cli")
	s = &session.Session{PID: pid, CWD: "/w", TmuxPaneID: "%1"}
	h.Enrich(s)
	if s.ID != "after-clear" || s.SessionFile != SessionFilePath("after-clear", "/w") {
		t.Fatalf("after the switch ID=%q file=%q, want after-clear and its transcript", s.ID, s.SessionFile)
	}
}
