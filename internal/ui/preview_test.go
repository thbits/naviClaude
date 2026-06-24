package ui

import (
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

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
