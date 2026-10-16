package claude

import (
	"fmt"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestParseResumeFlagInlineEquals_EdgeCases complements TestParseResumeFlagInlineForm
// in fixes_owned_test.go. It targets fix (1): the --resume=<uuid> inline form must
// split on the FIRST '=' only, must lose to nothing when the value is empty, and
// must keep working when surrounded by unrelated flags.
func TestParseResumeFlagInlineEquals_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		cmdLine string
		want    string
	}{
		{
			name:    "value itself contains an equals sign",
			cmdLine: "claude --resume=a=b=c",
			want:    "a=b=c",
		},
		{
			name:    "inline form is the very first token",
			cmdLine: "--resume=only-token",
			want:    "only-token",
		},
		{
			name:    "inline form preferred when it appears before a space form",
			cmdLine: "claude --resume=first --resume second",
			want:    "first",
		},
		{
			name:    "space form wins when it appears first",
			cmdLine: "claude --resume spaced --extra=ignored",
			want:    "spaced",
		},
		{
			name:    "no resume flag at all",
			cmdLine: "claude --continue --verbose",
			want:    "",
		},
		{
			name:    "resume-like prefix is not matched (must be exactly --resume=)",
			cmdLine: "claude --resumed=nope",
			want:    "",
		},
		{
			name:    "trailing whitespace around inline value is trimmed by Fields",
			cmdLine: "  claude   --resume=padded   ",
			want:    "padded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseResumeFlag(tt.cmdLine); got != tt.want {
				t.Errorf("parseResumeFlag(%q) = %q, want %q", tt.cmdLine, got, tt.want)
			}
		})
	}
}

// TestParseResumeFlagInlineForm covers the --resume=<uuid> form added alongside
// the existing space-separated --resume <uuid> form. (The space-separated cases
// are exercised by TestParseResumeFlag in detector_test.go.)
func TestParseResumeFlagInlineForm(t *testing.T) {
	tests := []struct {
		name    string
		cmdLine string
		want    string
	}{
		{
			name:    "inline equals form",
			cmdLine: "claude --resume=abc-123-def",
			want:    "abc-123-def",
		},
		{
			name:    "inline form with other args",
			cmdLine: "/usr/bin/claude --verbose --resume=my-session-id --json",
			want:    "my-session-id",
		},
		{
			name:    "inline form empty value",
			cmdLine: "claude --resume=",
			want:    "",
		},
		{
			name:    "inline form with uuid containing no extra equals",
			cmdLine: "claude --resume=11111111-2222-3333-4444-555555555555",
			want:    "11111111-2222-3333-4444-555555555555",
		},
		{
			name:    "space form still works after adding inline",
			cmdLine: "claude --resume space-id",
			want:    "space-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResumeFlag(tt.cmdLine)
			if got != tt.want {
				t.Errorf("parseResumeFlag(%q) = %q, want %q", tt.cmdLine, got, tt.want)
			}
		})
	}
}

// TestNativeStatusTrustedRegardlessOfTimestampAge is a regression test for a
// false-WAITING bug. Claude writes statusUpdatedAt only when the status CHANGES
// (event-driven), not as a heartbeat, so a session sitting stably idle or busy
// legitimately has an old timestamp -- an actively-busy session was observed
// with a 156s-old timestamp. resolveStatus must therefore honor a present
// native status regardless of its timestamp age; otherwise a stable session is
// demoted to the content-classification fallback, which can false-positive
// WAITING on idle pane content (the input box / leftover scrollback).
func TestNativeStatusTrustedRegardlessOfTimestampAge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := New(Options{})
	d := harness.NewDetector(nil, harness.NewRegistry(h), 0, 0)
	emptyTree := session.NewProcessTree(nil)
	// A timestamp far older than any plausible cutoff, with no tmux target so
	// status never comes from pane content -- the ONLY thing that can yield a
	// non-idle result is the trusted native status.
	staleMs := time.Now().Add(-30 * time.Minute).UnixMilli()
	cases := []struct {
		native string
		want   session.SessionStatus
	}{
		{"busy", session.StatusActive},
		{"idle", session.StatusIdle},
		{"waiting", session.StatusWaiting},
	}
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		pid := 7000 + i
		body := fmt.Sprintf(`{"pid":%d,"sessionId":"stale-%d","status":%q,"statusUpdatedAt":%d}`, pid, i, c.native, staleMs)
		if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &session.Session{PID: pid, ID: fmt.Sprintf("stale-%d", i)}
		if got := d.ResolveStatus(h, s, emptyTree); got != c.want {
			t.Errorf("native %q with a 30m-old timestamp: ResolveStatus = %v, want %v "+
				"(native status must be trusted regardless of age)", c.native, got, c.want)
		}
	}
}
