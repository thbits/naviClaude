package session

import "testing"

func TestSessionKey(t *testing.T) {
	tests := []struct {
		name string
		s    Session
		want string
	}{
		{"claude keeps bare id", Session{Harness: "claude", ID: "abc"}, "abc"},
		{"empty harness keeps bare id", Session{ID: "abc"}, "abc"},
		{"other harness is namespaced", Session{Harness: "codex", ID: "abc"}, "codex:abc"},
		{"empty id stays empty", Session{Harness: "codex"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.Key(); got != tt.want {
				t.Errorf("Key() = %q, want %q", got, tt.want)
			}
		})
	}
}
