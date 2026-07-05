package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/session"
)

// TestPreviewFitsHeightBudget guards against the header wrapping and stealing a
// row from the viewport: preview.View() must never exceed its height budget,
// otherwise the app's outer MaxHeight clips the last (bottom) content line.
// Regression test for the "last line not visible" bug.
func TestPreviewFitsHeightBudget(t *testing.T) {
	// A session with enough header content to overflow narrow widths and wrap.
	sess := &session.Session{
		ProjectName:  "some-long-project-name",
		GitBranch:    "feature/really-long-branch-name",
		Status:       session.StatusActive,
		TmuxTarget:   "workspace:12.3",
		CPU:          12.5,
		Memory:       456,
		LastActivity: time.Now(),
	}
	metrics := &session.SessionMetrics{StartTime: time.Now().Add(-90 * time.Minute), MessageCount: 42}

	var content strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&content, "line-%02d\n", i)
	}

	for _, tc := range []struct{ w, h int }{{40, 12}, {80, 20}, {120, 30}, {200, 50}} {
		for _, passthrough := range []bool{false, true} {
			p := NewPreview(tc.w, tc.h)
			p.SetSize(tc.w, tc.h)
			p.SetSession(sess)
			p.SetMetrics(metrics)
			p.SetPassthrough(passthrough)
			p.SetContent(content.String())

			if gotH := lipgloss.Height(p.View()); gotH > tc.h {
				t.Errorf("w=%d h=%d passthrough=%v: preview.View() height = %d, exceeds budget %d (header wrapped)",
					tc.w, tc.h, passthrough, gotH, tc.h)
			}
			// The header itself must stay at 2 rows (one line + bottom border).
			if hh := lipgloss.Height(p.renderHeader()); hh != 2 {
				t.Errorf("w=%d h=%d passthrough=%v: header height = %d, want 2", tc.w, tc.h, passthrough, hh)
			}
		}
	}
}

func TestCacheExpired(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		s    *session.Session
		want bool
	}{
		{
			name: "nil session",
			s:    nil,
			want: false,
		},
		{
			name: "closed session idle over threshold warns",
			s:    &session.Session{Status: session.StatusClosed, LastActivity: now.Add(-3 * time.Hour)},
			want: true,
		},
		{
			name: "closed session idle under threshold",
			s:    &session.Session{Status: session.StatusClosed, LastActivity: now.Add(-10 * time.Minute)},
			want: false,
		},
		{
			name: "zero last activity",
			s:    &session.Session{Status: session.StatusIdle},
			want: false,
		},
		{
			name: "live session idle under threshold",
			s:    &session.Session{Status: session.StatusActive, LastActivity: now.Add(-30 * time.Minute)},
			want: false,
		},
		{
			name: "live session idle just under threshold",
			s:    &session.Session{Status: session.StatusIdle, LastActivity: now.Add(-59 * time.Minute)},
			want: false,
		},
		{
			name: "live session idle exactly at threshold",
			s:    &session.Session{Status: session.StatusIdle, LastActivity: now.Add(-time.Hour)},
			want: true,
		},
		{
			name: "live session idle well over threshold",
			s:    &session.Session{Status: session.StatusWaiting, LastActivity: now.Add(-5 * time.Hour)},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cacheExpired(tt.s, now); got != tt.want {
				t.Errorf("cacheExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}
