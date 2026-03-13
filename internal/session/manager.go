package session

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/thbits/naviClaude/internal/tmux"
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
	return m.resumeWithFlags(sess, targetTmuxSession, false)
}

// ForkResume opens a new tmux window in targetTmuxSession and forks the given
// session using `claude --resume <sessionID> --fork-session`.
func (m *Manager) ForkResume(sess *Session, targetTmuxSession string) error {
	return m.resumeWithFlags(sess, targetTmuxSession, true)
}

func (m *Manager) resumeWithFlags(sess *Session, targetTmuxSession string, fork bool) error {
	if sess.ID == "" {
		return fmt.Errorf("resume: session has no ID")
	}
	cwd := sess.CWD
	if cwd == "" {
		cwd = "."
	}
	name := sess.ProjectName
	if name == "" {
		name = "claude"
	}
	cmd := fmt.Sprintf("cd %q && claude --resume %q", cwd, sess.ID)
	if fork {
		cmd += " --fork-session"
	}
	return m.tmuxClient.NewWindow(tmux.NewWindowOptions{
		Target:  targetTmuxSession + ":",
		Name:    name,
		Command: cmd,
	})
}

// Kill terminates an active session. Prefers tmux kill-pane (kills the pane
// and all processes in it). Falls back to SIGTERM on the process PID.
func (m *Manager) Kill(sess *Session) error {
	if sess.TmuxTarget != "" {
		return m.tmuxClient.KillPane(sess.TmuxTarget)
	}
	if sess.PID == 0 {
		return fmt.Errorf("kill: session %q has no PID or tmux target", sess.ID)
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

// CreateNewWithTarget is like CreateNew but returns the tmux target
// (e.g. "session:3.0") of the newly created pane.
func (m *Manager) CreateNewWithTarget(cwd string, targetTmuxSession string) (string, error) {
	if cwd == "" {
		cwd = "."
	}
	name := filepath.Base(cwd)
	if name == "" || name == "." {
		name = "claude"
	}
	cmd := fmt.Sprintf("cd %q && claude", cwd)
	return m.tmuxClient.NewWindowPrint(tmux.NewWindowOptions{
		Target:  targetTmuxSession + ":",
		Name:    name,
		Command: cmd,
	})
}
