package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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
// render paths carry every field (identity, status, target, uptime, model, CPU, MEM).
func previewWithData(passthrough bool) PreviewModel {
	m := NewPreview(100, 12)
	m.SetSession(&session.Session{
		ID:           "p1",
		ProjectName:  "myproj",
		GitBranch:    "main",
		Status:       session.StatusActive,
		TmuxTarget:   "work:1.0",
		CPU:          3.2,
		Memory:       210,
		Model:        "opus",
		LastActivity: time.Now().Add(-5 * time.Minute),
	})
	m.SetMetrics("p1", &session.SessionMetrics{StartTime: time.Now().Add(-2 * time.Hour), MessageCount: 47, ModelID: "claude-opus-5-5[1m]"})
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
	for _, want := range []string{"▸ myproj", "main", "ACTIVE", "⏱ 2h", "Opus 5.5", "CPU 3.2%", "MEM 210MB"} {
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
	for _, want := range []string{"myproj", "main", "ACTIVE", "⏱ 2h", "Opus 5.5", "CPU 3.2%", "MEM 210MB"} {
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

// The header leaves message counts and last-activity time to the sidebar row
// and detail popup, in both render paths.
func TestPreviewHeaderOmitsDuplicatedFields(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		header := previewWithData(passthrough).renderHeader()
		for _, unwanted := range []string{"msgs", "47", "5m"} {
			if strings.Contains(header, unwanted) {
				t.Errorf("passthrough=%v: header should not contain %q; got:\n%s", passthrough, unwanted, header)
			}
		}
		// Right side order: uptime, model, CPU, MEM.
		line := firstLines(header, 1)[0]
		up, model, cpu, mem := strings.Index(line, "⏱ 2h"), strings.Index(line, "Opus 5.5"), strings.Index(line, "CPU"), strings.Index(line, "MEM")
		if !(up >= 0 && up < model && model < cpu && cpu < mem) {
			t.Errorf("passthrough=%v: want uptime < model < CPU < MEM, got %d %d %d %d in %q", passthrough, up, model, cpu, mem, line)
		}
	}
}

// Without a known model the header simply skips that field.
func TestPreviewHeaderWithoutModel(t *testing.T) {
	m := previewWithData(false)
	s := *m.session
	s.Model = ""
	m.SetSession(&s)
	metrics := *m.metrics
	metrics.ModelID = ""
	m.SetMetrics("p1", &metrics)
	if h := m.renderHeader(); !strings.Contains(h, "⏱ 2h • CPU 3.2%") {
		t.Errorf("header should go straight from uptime to CPU; got:\n%s", h)
	}
}

// Before metrics load (or for an unparsable id) the header shows the family.
func TestPreviewHeaderModelFallsBackToFamily(t *testing.T) {
	m := previewWithData(false)
	m.SetMetrics("p1", nil)
	if h := m.renderHeader(); !strings.Contains(h, " Opus • CPU") {
		t.Errorf("header should show the bare family; got:\n%s", h)
	}
}

func TestModelLabel(t *testing.T) {
	tests := []struct {
		id, family, want string
	}{
		{"claude-opus-5-5", "opus", "Opus 5.5"},
		{"claude-sonnet-5", "sonnet", "Sonnet 5"},
		{"claude-haiku-5-5", "haiku", "Haiku 5.5"},
		{"claude-fable-5-1", "fable", "Fable 5.1"},
		{"claude-opus-4-8", "opus", "Opus 4.8"},
		{"claude-3-5-sonnet-20241022", "sonnet", "Sonnet 3.5"},
		{"claude-opus-5-5[1m]", "opus", "Opus 5.5"},
		{"claude-sonnet-4-5-20250929[1m]", "sonnet", "Sonnet 4.5"},
		{"claude-opus-4-20250514", "opus", "Opus 4"},
		{"claude-3-opus-20240229", "opus", "Opus 3"},
		{"us.anthropic.claude-3-5-sonnet-20241022-v2:0", "sonnet", "Sonnet 3.5"},
		{"", "OPUS", "Opus"},
		{"gpt-5", "sonnet", "Sonnet"},
		{"claude-", "haiku", "Haiku"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := modelLabel(tt.id, tt.family); got != tt.want {
			t.Errorf("modelLabel(%q, %q) = %q, want %q", tt.id, tt.family, got, tt.want)
		}
	}
}

// Metrics loaded for a different session must not label this header.
func TestPreviewHeaderIgnoresOtherSessionsMetrics(t *testing.T) {
	m := previewWithData(false)
	m.SetMetrics("other", &session.SessionMetrics{StartTime: time.Now().Add(-2 * time.Hour), ModelID: "claude-haiku-5-5"})
	if h := ansi.Strip(m.renderHeader()); strings.Contains(h, "⏱") || strings.Contains(h, "Haiku") {
		t.Errorf("header shows another session's metrics:\n%s", h)
	}
}

func TestGroupSummaryModelsLabeledAndSorted(t *testing.T) {
	sessions := []*session.Session{
		{ID: "1", Model: "sonnet", ProjectName: "b", Status: session.StatusIdle},
		{ID: "2", Model: "opus", ProjectName: "a", Status: session.StatusIdle},
		{ID: "3", Model: "claude-haiku-5-5", ProjectName: "c", Status: session.StatusIdle},
		{ID: "4", Model: "opus", ProjectName: "a", Status: session.StatusIdle},
	}
	m := NewPreview(80, 30)
	m.SetSize(80, 30)
	var first string
	for i := 0; i < 20; i++ {
		m.SetGroupSummary("grp", sessions)
		if i == 0 {
			first = ansi.Strip(m.content)
		} else if got := ansi.Strip(m.content); got != first {
			t.Fatalf("summary order changed between renders:\n%s\n---\n%s", first, got)
		}
	}
	want := "Models\n    2 Opus\n    1 Haiku 5.5\n    1 Sonnet\n"
	if !strings.Contains(first, want) {
		t.Errorf("want models block %q in\n%s", want, first)
	}
}
