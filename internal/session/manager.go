package session

import (
	"fmt"
	"os/exec"
	"strconv"

	"github.com/tomhalo/naviclaude/internal/tmux"
)

// Manager performs lifecycle operations on Claude sessions: creating new ones,
// resuming closed ones, forking existing ones, and killing running ones.
type Manager struct {
	tmuxClient *tmux.Client
}

// NewManager creates a Manager backed by the given tmux client.
func NewManager(client *tmux.Client) *Manager {
	return &Manager{tmuxClient: client}
}

// Resume opens a new tmux window in targetTmuxSession and resumes the given
// closed session using `claude --resume <sessionID>`.
func (m *Manager) Resume(sess *Session, targetTmuxSession string) error {
	if sess.ID == "" {
		return fmt.Errorf("resume: session has no ID")
	}
	cwd := sess.CWD
	if cwd == "" {
		cwd = "."
	}
	cmd := fmt.Sprintf("cd %q && claude --resume %s", cwd, sess.ID)
	return m.tmuxClient.NewWindow(tmux.NewWindowOptions{
		Target:  targetTmuxSession + ":",
		Command: cmd,
	})
}

// ForkResume opens a new tmux window in targetTmuxSession and forks the given
// session using `claude --resume <sessionID> --fork-session`.
func (m *Manager) ForkResume(sess *Session, targetTmuxSession string) error {
	if sess.ID == "" {
		return fmt.Errorf("fork-resume: session has no ID")
	}
	cwd := sess.CWD
	if cwd == "" {
		cwd = "."
	}
	cmd := fmt.Sprintf("cd %q && claude --resume %s --fork-session", cwd, sess.ID)
	return m.tmuxClient.NewWindow(tmux.NewWindowOptions{
		Target:  targetTmuxSession + ":",
		Command: cmd,
	})
}

// Kill sends SIGTERM to the process associated with an active session.
func (m *Manager) Kill(sess *Session) error {
	if sess.PID == 0 {
		return fmt.Errorf("kill: session %q has no PID", sess.ID)
	}
	if err := exec.Command("kill", strconv.Itoa(sess.PID)).Run(); err != nil {
		return fmt.Errorf("kill process %d: %w", sess.PID, err)
	}
	return nil
}

// CreateNew opens a new tmux window in targetTmuxSession, changes to cwd, and
// starts a fresh Claude Code session.
func (m *Manager) CreateNew(cwd string, targetTmuxSession string) error {
	if cwd == "" {
		cwd = "."
	}
	cmd := fmt.Sprintf("cd %q && claude", cwd)
	return m.tmuxClient.NewWindow(tmux.NewWindowOptions{
		Target:  targetTmuxSession + ":",
		Command: cmd,
	})
}
