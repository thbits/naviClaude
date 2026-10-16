package claude

import (
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

func TestResumeAndForkCommands(t *testing.T) {
	h := New(Options{Command: "cc"})
	s := &session.Session{ID: "abc-123"}
	if got, want := h.ResumeCommand(s), `claude --resume "abc-123"`; got != want {
		t.Errorf("ResumeCommand = %q, want %q", got, want)
	}
	if got, want := h.ForkCommand(s), `claude --resume "abc-123" --fork-session`; got != want {
		t.Errorf("ForkCommand = %q, want %q", got, want)
	}
}

func TestInfo(t *testing.T) {
	info := New(Options{}).Info()
	if info.Name != "claude" || info.Label != "Claude" || info.AssistantLabel != "Claude" {
		t.Errorf("Info = %+v", info)
	}
	if info.PromptCacheTTL.Hours() != 1 {
		t.Errorf("PromptCacheTTL = %v, want 1h", info.PromptCacheTTL)
	}
}
