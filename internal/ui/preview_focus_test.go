package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

func previewWithSession() PreviewModel {
	m := NewPreview(60, 12)
	m.SetSession(&session.Session{
		ProjectName: "myproj",
		TmuxTarget:  "work:1.0",
		Status:      session.StatusActive,
	})
	return m
}

// previewWithData builds a wide preview with a full metrics set, so both header
// render paths carry every field (identity, status, target, uptime, msgs, CPU, MEM).
func previewWithData(passthrough bool) PreviewModel {
	m := NewPreview(100, 12)
	m.SetSession(&session.Session{
		ProjectName:  "myproj",
		GitBranch:    "main",
		Status:       session.StatusActive,
		TmuxTarget:   "work:1.0",
		CPU:          3.2,
		Memory:       210,
		LastActivity: time.Now(),
	})
	m.SetMetrics(&session.SessionMetrics{StartTime: time.Now().Add(-2 * time.Hour), MessageCount: 47})
	m.SetSize(100, 12)
	m.SetPassthrough(passthrough)
	return m
}

// The two-tone focused band must keep every data field (only the coloring
// differs from the unfocused path); a regression that dropped the metrics band
// would strip these. Color is not asserted: the test environment has no TTY, so
// lipgloss emits plain text — this checks content and layout, not escapes.
func TestPreviewFocusedHeaderKeepsAllData(t *testing.T) {
	v := previewWithData(true).View()
	for _, want := range []string{"▸ myproj", "main", "ACTIVE", "msgs 47", "CPU 3.2%", "MEM 210MB"} {
		if !strings.Contains(v, want) {
			t.Errorf("focused header missing %q; got:\n%s", want, v)
		}
	}
	t.Logf("focused header layout (no color in test env):\n%s", strings.Join(firstLines(v, 2), "\n"))
}

// The unfocused header shows the same data but flat (no identity chip); it
// dims via color at runtime, which is not observable here.
func TestPreviewUnfocusedHeaderKeepsAllDataFlat(t *testing.T) {
	v := previewWithData(false).View()
	if strings.Contains(v, "▸") {
		t.Errorf("unfocused header must not show the focus marker; got:\n%s", v)
	}
	for _, want := range []string{"myproj", "main", "ACTIVE", "msgs 47", "CPU 3.2%", "MEM 210MB"} {
		if !strings.Contains(v, want) {
			t.Errorf("unfocused header missing %q; got:\n%s", want, v)
		}
	}
	t.Logf("unfocused header layout (no color in test env):\n%s", strings.Join(firstLines(v, 2), "\n"))
}

func firstLines(s string, n int) []string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return lines
}

func TestPreviewShowsFocusChipWhenPassthrough(t *testing.T) {
	m := previewWithSession()
	m.SetPassthrough(true)
	if !strings.Contains(m.View(), "▸ myproj") {
		t.Fatalf("focused preview header should contain the lit chip; got:\n%s", m.View())
	}
}

func TestPreviewHasNoFocusChipWhenNotPassthrough(t *testing.T) {
	m := previewWithSession()
	m.SetPassthrough(false)
	v := m.View()
	if strings.Contains(v, "▸ myproj") {
		t.Fatalf("unfocused preview header must not show the focus chip; got:\n%s", v)
	}
	if !strings.Contains(v, "myproj") {
		t.Fatalf("preview header should still show the project name; got:\n%s", v)
	}
}
