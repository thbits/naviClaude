package claude

import (
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeSessionFixture writes a ~/.claude/sessions/<pid>.json file under a fake
// HOME for tests. The caller has already set HOME via t.Setenv.
func writeSessionFixture(t *testing.T, home string, pid int, sessionID, status string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"pid":` + strconv.Itoa(pid) + `,"sessionId":"` + sessionID + `","status":"` + status + `"}`
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReadNativeStateByID covers the sessionId lookup used when the matched PID
// is a launcher that owns no <pid>.json.
func TestReadNativeStateByID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	live, other := os.Getpid(), os.Getppid()
	writeSessionFixture(t, home, live, "uuid-idle", "idle")
	writeSessionFixture(t, home, other, "uuid-busy", "busy")

	t.Run("falls back to sessionId when the PID has no file", func(t *testing.T) {
		st, ok := ReadNativeState(deadPID, "uuid-idle")
		if !ok || st.Status != "idle" {
			t.Errorf("ReadNativeState(dead, uuid-idle) = (%+v,%v), want idle", st, ok)
		}
	})

	t.Run("uses the PID file when present", func(t *testing.T) {
		st, ok := ReadNativeState(other, "")
		if !ok || st.Status != "busy" {
			t.Errorf("ReadNativeState(other, \"\") = (%+v,%v), want busy", st, ok)
		}
	})

	t.Run("not found when neither matches", func(t *testing.T) {
		if st, ok := ReadNativeState(deadPID, "uuid-missing"); ok {
			t.Errorf("ReadNativeState(dead, uuid-missing) = (%+v,true), want not found", st)
		}
	})

	t.Run("skips a file left behind by a dead process", func(t *testing.T) {
		writeSessionFixture(t, home, deadPID, "uuid-crashed", "busy")
		if st, ok := ReadNativeState(0, "uuid-crashed"); ok {
			t.Errorf("ReadNativeState(0, uuid-crashed) = (%+v,true), want the stale file ignored", st)
		}
	})
}

// writeNamedSessionFixture writes a ~/.claude/sessions/<pid>.json file that also
// carries Claude's own session "name" field, under a fake HOME for tests.
func writeNamedSessionFixture(t *testing.T, home string, pid int, sessionID, name, status string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"pid":` + strconv.Itoa(pid) + `,"sessionId":"` + sessionID +
		`","name":"` + name + `","status":"` + status + `"}`
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReadSessionMetadataByID covers recovering a live session's name by
// sessionId when the matched PID is a launcher.
func TestReadSessionMetadataByID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeNamedSessionFixture(t, home, os.Getpid(), "uuid-named", "squash-merge-session-status", "busy")

	t.Run("recovers name and status by sessionId", func(t *testing.T) {
		m, ok := readSessionMetadataByID("uuid-named")
		if !ok {
			t.Fatal("readSessionMetadataByID(uuid-named) not found, want found")
		}
		if m.Name != "squash-merge-session-status" {
			t.Errorf("Name = %q, want %q", m.Name, "squash-merge-session-status")
		}
		if m.Status != "busy" {
			t.Errorf("Status = %q, want %q", m.Status, "busy")
		}
	})

	t.Run("unknown sessionId returns not found", func(t *testing.T) {
		if _, ok := readSessionMetadataByID("uuid-missing"); ok {
			t.Error("readSessionMetadataByID(uuid-missing) = found, want not found")
		}
	})

	t.Run("empty sessionId returns not found", func(t *testing.T) {
		if _, ok := readSessionMetadataByID(""); ok {
			t.Error("readSessionMetadataByID(\"\") = found, want not found")
		}
	})

}

func TestMapNativeStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   session.SessionStatus
		wantOK bool
	}{
		{"busy is active", "busy", session.StatusActive, true},
		{"waiting is waiting", "waiting", session.StatusWaiting, true},
		{"idle is idle", "idle", session.StatusIdle, true},
		{"shell folds into idle", "shell", session.StatusIdle, true},
		{"empty is not authoritative", "", session.StatusIdle, false},
		{"unknown is not authoritative", "running", session.StatusIdle, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mapNativeStatus(tt.status)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("mapNativeStatus(%q) = (%v, %v), want (%v, %v)",
					tt.status, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestParseSessionMetadata(t *testing.T) {
	t.Run("current v2.1.190 record", func(t *testing.T) {
		data := []byte(`{"pid":15319,"sessionId":"9eebde83-ddd4","cwd":"/x",` +
			`"version":"2.1.190","kind":"interactive","status":"busy",` +
			`"updatedAt":1782334180285,"statusUpdatedAt":1782334180285}`)
		meta := parseSessionMetadata(data)
		if meta.SessionID != "9eebde83-ddd4" {
			t.Errorf("SessionID = %q, want %q", meta.SessionID, "9eebde83-ddd4")
		}
		if meta.Status != "busy" {
			t.Errorf("Status = %q, want %q", meta.Status, "busy")
		}
		if meta.StatusUpdatedAt != 1782334180285 {
			t.Errorf("StatusUpdatedAt = %d, want %d", meta.StatusUpdatedAt, 1782334180285)
		}
	})

	t.Run("older record without status field", func(t *testing.T) {
		data := []byte(`{"pid":111,"sessionId":"old-uuid","name":"my session"}`)
		meta := parseSessionMetadata(data)
		if meta.SessionID != "old-uuid" {
			t.Errorf("SessionID = %q, want %q", meta.SessionID, "old-uuid")
		}
		if meta.Name != "my session" {
			t.Errorf("Name = %q, want %q", meta.Name, "my session")
		}
		if meta.Status != "" {
			t.Errorf("Status = %q, want empty", meta.Status)
		}
		// An empty status must not be treated as authoritative.
		if _, ok := mapNativeStatus(meta.Status); ok {
			t.Error("empty status from old record should not be authoritative")
		}
	})

	t.Run("malformed json yields zero struct", func(t *testing.T) {
		meta := parseSessionMetadata([]byte(`{not valid json`))
		if meta != (sessionMetadata{}) {
			t.Errorf("malformed JSON = %+v, want zero struct", meta)
		}
	})
}

// TestResolveStatusNativeFirst verifies the native status short-circuits the
// signal-based fallback: a stashed native status is returned verbatim regardless
// of CPU, and an absent native status falls through to the legacy logic.
func TestResolveStatusNativeFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := New(Options{})

	// The session PID burns CPU well above the active threshold, which the
	// fallback path would read as "working".
	busyTree := session.NewProcessTree([]session.Proc{{PID: 42, PPID: 1, Name: "claude", CPU: 99.0, RSSKB: 1024}})

	// Native status must win even when CPU contradicts it. Each subtest uses a
	// fresh detector so the per-target hysteresis tracker starts empty.
	nativeCases := []struct {
		name   string
		status string
		want   session.SessionStatus
	}{
		{"native idle wins over high CPU", "idle", session.StatusIdle},
		{"native shell folds to idle and wins over high CPU", "shell", session.StatusIdle},
		{"native waiting wins", "waiting", session.StatusWaiting},
		{"native busy is active", "busy", session.StatusActive},
	}
	for _, tt := range nativeCases {
		t.Run(tt.name, func(t *testing.T) {
			writeSessionFixture(t, home, 42, "uuid-42", tt.status)
			d := harness.NewDetector(nil, harness.NewRegistry(h), 5, 5.0)
			s := &session.Session{PID: 42, ID: "uuid-42"}
			if got := d.ResolveStatus(h, s, busyTree); got != tt.want {
				t.Errorf("ResolveStatus = %v, want %v (native must override signals)", got, tt.want)
			}
		})
	}

	if err := os.Remove(filepath.Join(home, ".claude", "sessions", "42.json")); err != nil {
		t.Fatal(err)
	}

	t.Run("no native status falls back to CPU signal", func(t *testing.T) {
		// Empty TmuxTarget => content classification is skipped (no tmux call);
		// CPU above the threshold should drive the fallback to Active.
		d := harness.NewDetector(nil, harness.NewRegistry(h), 5, 5.0)
		s := &session.Session{PID: 42}
		if got := d.ResolveStatus(h, s, busyTree); got != session.StatusActive {
			t.Errorf("ResolveStatus = %v, want StatusActive (fallback CPU signal)", got)
		}
	})

	t.Run("no native status and no activity falls back to idle", func(t *testing.T) {
		d := harness.NewDetector(nil, harness.NewRegistry(h), 5, 5.0)
		idleTree := session.NewProcessTree([]session.Proc{{PID: 43, PPID: 1, Name: "claude", RSSKB: 1024}})
		s := &session.Session{PID: 43}
		if got := d.ResolveStatus(h, s, idleTree); got != session.StatusIdle {
			t.Errorf("ResolveStatus = %v, want StatusIdle (fallback, no activity)", got)
		}
	})
}
