package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func writeLiveMeta(t *testing.T, home string, pid int, sessionID, tmux string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"pid":` + strconv.Itoa(pid) + `,"sessionId":"` + sessionID + `","tmux":"` + tmux + `"}`
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCwdTranscript(t *testing.T, home, cwd, id string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", cwdSlug(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

const deadPID = 999999

func TestFallbackFindsMetadataByPane(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeLiveMeta(t, home, os.Getpid(), "own", "work:@3.%9")

	if got := extractSessionIDFallback(deadPID, "/w", "%9"); got != "own" {
		t.Errorf("got %q, want the metadata file recorded for pane %%9", got)
	}
}

func TestFallbackDoesNotBorrowAnotherSessionsID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Now()
	writeLiveMeta(t, home, os.Getpid(), "busy-neighbor", "work:@1.%1")
	writeCwdTranscript(t, home, "/w", "busy-neighbor", now)
	writeCwdTranscript(t, home, "/w", "long-closed", now.Add(-time.Hour))

	if got := extractSessionIDFallback(os.Getpid(), "/w", "%9"); got != "" {
		t.Errorf("got %q, want no ID until the new session writes its own transcript", got)
	}

	writeCwdTranscript(t, home, "/w", "mine", now.Add(time.Second))
	if got := extractSessionIDFallback(os.Getpid(), "/w", "%9"); got != "mine" {
		t.Errorf("got %q, want the session's own transcript once it exists", got)
	}
}

func TestTmuxPaneID(t *testing.T) {
	for in, want := range map[string]string{
		"naviclaude:@5.%5": "%5",
		"my.sess:@12.%103": "%103",
		"":                 "",
		"no-pane":          "",
	} {
		if got := tmuxPaneID(in); got != want {
			t.Errorf("tmuxPaneID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProcessStartIsPast(t *testing.T) {
	st := processStart(os.Getpid())
	if st.IsZero() || st.After(time.Now()) {
		t.Errorf("processStart(self) = %v, want a past time", st)
	}
	if !processStart(deadPID).IsZero() {
		t.Error("a dead PID must have no start time")
	}
}
