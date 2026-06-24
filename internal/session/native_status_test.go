package session

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/thbits/naviClaude/internal/tmux"
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

// TestNativeStatusByIDFallback covers the sessionId-based lookup that makes the
// detector robust to the picked PID not owning the <pid>.json file (the CLI's
// launcher/wrapper process layering).
func TestNativeStatusByIDFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// File is keyed by PID 12169 but the picked PID will be different (9999).
	writeSessionFixture(t, home, 12169, "uuid-idle", "idle")
	writeSessionFixture(t, home, 15319, "uuid-busy", "busy")

	t.Run("readSessionStatusByID finds by sessionId", func(t *testing.T) {
		got, ok := readSessionStatusByID("uuid-idle")
		if !ok || got != "idle" {
			t.Errorf("readSessionStatusByID(uuid-idle) = (%q,%v), want (idle,true)", got, ok)
		}
	})

	t.Run("unknown sessionId returns not found", func(t *testing.T) {
		if got, ok := readSessionStatusByID("uuid-missing"); ok {
			t.Errorf("readSessionStatusByID(uuid-missing) = (%q,true), want not found", got)
		}
	})

	t.Run("empty sessionId returns not found", func(t *testing.T) {
		if _, ok := readSessionStatusByID(""); ok {
			t.Error("readSessionStatusByID(\"\") should not be found")
		}
	})

	t.Run("NativeStatus falls back to sessionId when PID file absent", func(t *testing.T) {
		// PID 9999 has no file; the sessionId must drive the result.
		got, ok := NativeStatus(9999, "uuid-idle")
		if !ok || got != StatusIdle {
			t.Errorf("NativeStatus(9999, uuid-idle) = (%v,%v), want (Idle,true)", got, ok)
		}
	})

	t.Run("NativeStatus uses PID file when present", func(t *testing.T) {
		got, ok := NativeStatus(15319, "")
		if !ok || got != StatusActive {
			t.Errorf("NativeStatus(15319, \"\") = (%v,%v), want (Active,true)", got, ok)
		}
	})

	t.Run("NativeStatus not found when neither matches", func(t *testing.T) {
		if got, ok := NativeStatus(9999, "uuid-missing"); ok {
			t.Errorf("NativeStatus(9999, uuid-missing) = (%v,true), want not found", got)
		}
	})
}

func TestMapNativeStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   SessionStatus
		wantOK bool
	}{
		{"busy is active", "busy", StatusActive, true},
		{"waiting is waiting", "waiting", StatusWaiting, true},
		{"idle is idle", "idle", StatusIdle, true},
		{"shell folds into idle", "shell", StatusIdle, true},
		{"empty is not authoritative", "", StatusIdle, false},
		{"unknown is not authoritative", "running", StatusIdle, false},
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
	// A tree where the session PID burns CPU well above the active threshold,
	// which the fallback path would read as "working".
	busyTree := &ProcessTree{
		children: map[int][]int{},
		names:    map[int]string{42: "claude"},
		ppid:     map[int]int{42: 1},
		cpu:      map[int]float64{42: 99.0},
		rss:      map[int]float64{42: 1024},
	}

	// Native status must win even when CPU contradicts it. Each subtest uses a
	// fresh detector so the per-target hysteresis tracker starts empty.
	nativeCases := []struct {
		name   string
		status string
		want   SessionStatus
	}{
		{"native idle wins over high CPU", "idle", StatusIdle},
		{"native shell folds to idle and wins over high CPU", "shell", StatusIdle},
		{"native waiting wins", "waiting", StatusWaiting},
		{"native busy is active", "busy", StatusActive},
	}
	for _, tt := range nativeCases {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDetector(&tmux.Client{}, nil, 5, 5.0)
			s := &Session{PID: 42, nativeStatus: tt.status}
			if got := d.resolveStatus(s, busyTree); got != tt.want {
				t.Errorf("resolveStatus = %v, want %v (native must override signals)", got, tt.want)
			}
		})
	}

	t.Run("no native status falls back to CPU signal", func(t *testing.T) {
		// Empty TmuxTarget => content classification is skipped (no tmux call);
		// CPU above the threshold should drive the fallback to Active.
		d := NewDetector(&tmux.Client{}, nil, 5, 5.0)
		s := &Session{PID: 42, nativeStatus: ""}
		if got := d.resolveStatus(s, busyTree); got != StatusActive {
			t.Errorf("resolveStatus = %v, want StatusActive (fallback CPU signal)", got)
		}
	})

	t.Run("no native status and no activity falls back to idle", func(t *testing.T) {
		// Fresh detector + empty target: no content, no CPU, no transcript, and an
		// empty hysteresis tracker => idle.
		d := NewDetector(&tmux.Client{}, nil, 5, 5.0)
		idleTree := &ProcessTree{
			children: map[int][]int{},
			names:    map[int]string{43: "claude"},
			ppid:     map[int]int{43: 1},
			cpu:      map[int]float64{43: 0.0},
			rss:      map[int]float64{43: 1024},
		}
		s := &Session{PID: 43, nativeStatus: ""}
		if got := d.resolveStatus(s, idleTree); got != StatusIdle {
			t.Errorf("resolveStatus = %v, want StatusIdle (fallback, no activity)", got)
		}
	})
}
