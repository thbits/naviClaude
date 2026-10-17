package app

import (
	"fmt"

	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// harnessFor returns the harness owning s, or nil when unknown.
func (m Model) harnessFor(s *session.Session) harness.Harness {
	if m.registry == nil {
		return nil
	}
	h, ok := m.registry.For(s)
	if !ok {
		return nil
	}
	return h
}

// defaultInfo describes the harness used for new sessions.
func (m Model) defaultInfo() harness.Info {
	if m.registry == nil {
		return harness.Info{}
	}
	return m.registry.Default().Info()
}

// menuCaps reports which lifecycle actions the context menu may offer for s.
func (m Model) menuCaps(s *session.Session) ui.MenuCaps {
	h := m.harnessFor(s)
	if h == nil {
		return ui.MenuCaps{}
	}
	_, canResume := harness.AsResumer(h)
	_, canFork := harness.AsForker(h)
	return ui.MenuCaps{CanResume: canResume, CanFork: canFork}
}

// resumeCommandFor returns the shell command that resumes s (forked when fork).
func resumeCommandFor(h harness.Harness, s *session.Session, fork bool) (string, error) {
	if h == nil {
		return "", fmt.Errorf("unknown harness %q", s.Harness)
	}
	if fork {
		if f, ok := harness.AsForker(h); ok {
			return f.ForkCommand(s), nil
		}
		return "", fmt.Errorf("%s does not support fork-resume", h.Info().Label)
	}
	if r, ok := harness.AsResumer(h); ok {
		return r.ResumeCommand(s), nil
	}
	return "", fmt.Errorf("%s does not support resume", h.Info().Label)
}

// partitionByHarness groups sessions by owning harness name; sessions with an
// unknown harness are dropped.
func partitionByHarness(reg *harness.Registry, list []*session.Session) map[string][]*session.Session {
	out := make(map[string][]*session.Session)
	if reg == nil {
		return out
	}
	for _, s := range list {
		if h, ok := reg.For(s); ok {
			name := h.Info().Name
			out[name] = append(out[name], s)
		}
	}
	return out
}

// contextLimitFunc sizes a session's context window through its harness.
func contextLimitFunc(reg *harness.Registry) ui.ContextLimitFunc {
	return func(s *session.Session, modelID string, tokensUsed int) int {
		h, ok := reg.For(s)
		if !ok {
			return 0
		}
		cl, ok := harness.AsContextLimiter(h)
		if !ok {
			return 0
		}
		return cl.ContextLimit(modelID, s.Model, tokensUsed)
	}
}
