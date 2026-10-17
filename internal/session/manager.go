package session

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/thbits/naviClaude/internal/tmux"
)

// Manager creates, resumes and kills agent sessions in tmux; harnesses supply
// the shell commands.
type Manager struct {
	tmuxClient *tmux.Client
	windowName string // window-name template; {project} and {harness} expand
}

// NewManager creates a Manager backed by the given tmux client.
func NewManager(client *tmux.Client, windowName string) *Manager {
	return &Manager{tmuxClient: client, windowName: windowName}
}

// nameWindow fills the window-name template; an unusable project name falls
// back to the harness name.
func (m *Manager) nameWindow(project, harness string) string {
	if project == "" || project == "." || project == "/" {
		project = harness
	}
	format := m.windowName
	if format == "" {
		format = "{project}"
	}
	name := strings.TrimSpace(strings.NewReplacer("{project}", project, "{harness}", harness).Replace(format))
	if name == "" {
		return "session"
	}
	return name
}

// shellSingleQuote quotes s as one literal shell word, where %q would still expand
// $ and backticks. Paths only: the user's start command is meant to be evaluated.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Resume opens a new tmux window in targetTmuxSession that cd's into the
// session's directory and runs command (a harness resume or fork command).
func (m *Manager) Resume(sess *Session, targetTmuxSession, command string) error {
	if sess.ID == "" {
		return fmt.Errorf("resume: session has no ID")
	}
	return m.tmuxClient.NewWindow(tmux.NewWindowOptions{
		Target:  targetTmuxSession + ":",
		Name:    m.nameWindow(sess.ProjectName, sess.Harness),
		Command: resumeShellCommand(sess.CWD, command),
	})
}

// resumeShellCommand cd's into cwd (default ".") and runs command. The cwd is
// single-quoted to suppress shell expansion.
func resumeShellCommand(cwd, command string) string {
	if cwd == "" {
		cwd = "."
	}
	return fmt.Sprintf("cd %s && %s", shellSingleQuote(cwd), command)
}

// ResumeInSession resumes sess in tmux session targetName, creating it if missing,
// and returns the pane target; a new session gets the command via send-keys.
func (m *Manager) ResumeInSession(sess *Session, targetName, command string) (string, error) {
	if sess.ID == "" {
		return "", fmt.Errorf("resume: session has no ID")
	}
	if targetName == "" {
		return "", fmt.Errorf("resume: target session name is empty")
	}
	cmd := resumeShellCommand(sess.CWD, command)

	if m.tmuxClient.HasSession(targetName) {
		// Existing session: open the resume in a new window and return its target.
		target, err := m.tmuxClient.NewWindowPrint(tmux.NewWindowOptions{
			Target:  targetName + ":",
			Name:    m.nameWindow(sess.ProjectName, sess.Harness),
			Command: cmd,
		})
		if err != nil {
			return "", fmt.Errorf("create window: %w", err)
		}
		return target, nil
	}

	cwd := sess.CWD
	if cwd == "" {
		cwd = "."
	}
	target, err := m.tmuxClient.NewSessionPrint(tmux.NewSessionOptions{
		Name:       targetName,
		WindowName: m.nameWindow(sess.ProjectName, sess.Harness),
		StartDir:   cwd,
	})
	if err != nil {
		return "", fmt.Errorf("create tmux session: %w", err)
	}
	// Resize to match the current client (detached sessions default to 80x24).
	// Errors are non-fatal.
	m.tmuxClient.ResizeToClient(targetName)

	if err := m.tmuxClient.SendKeys(target, cmd); err != nil {
		return "", fmt.Errorf("send resume command: %w", err)
	}
	if err := m.tmuxClient.SendKeys(target, "Enter"); err != nil {
		return "", fmt.Errorf("send enter: %w", err)
	}
	return target, nil
}

// quitTimeout bounds how long Kill waits for the agent to run its exit hooks
// before force-closing the pane.
const quitTimeout = 15 * time.Second

// Kill SIGTERMs the agent so its SessionEnd hooks run, waits for it to exit, then
// closes its pane unless a parent app (e.g. nvim) owns it.
func (m *Manager) Kill(sess *Session) error {
	pane := sess.TmuxPaneID
	if pane == "" {
		pane = sess.TmuxTarget
	}
	if sess.PID == 0 {
		if pane == "" {
			return fmt.Errorf("kill: session %q has no PID or tmux target", sess.ID)
		}
		return m.tmuxClient.KillPane(pane)
	}
	if err := syscall.Kill(sess.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("terminate process %d: %w", sess.PID, err)
	}
	waitForExit(sess.PID, quitTimeout)
	if pane == "" || sess.Subprocess || !m.tmuxClient.PaneExists(pane) {
		return nil
	}
	return m.tmuxClient.KillPane(pane)
}

// waitForExit polls until pid is gone or timeout elapses, reporting whether
// it exited.
func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// CreateNewTmuxSession starts the agent in a detached tmux session named
// sessionName (else cwd's basename, else fallbackName) and returns the pane target.
func (m *Manager) CreateNewTmuxSession(cwd, command, sessionName, fallbackName string) (tmuxSession, target string, err error) {
	if cwd == "" {
		cwd = "."
	}
	base := filepath.Base(cwd)
	if base == "" || base == "." || base == "/" {
		base = fallbackName
	}
	if sessionName == "" {
		sessionName = base
	}

	// Reuse an existing session by name rather than failing as a duplicate.
	if m.tmuxClient.HasSession(sessionName) {
		target, err = m.CreateNewWithTarget(cwd, sessionName, command, fallbackName)
		if err != nil {
			return "", "", err
		}
		return sessionName, target, nil
	}

	target, err = m.tmuxClient.NewSessionPrint(tmux.NewSessionOptions{
		Name:       sessionName,
		WindowName: m.nameWindow(base, fallbackName),
		StartDir:   cwd,
	})
	if err == nil {
		// Resize to match the current client so the pane isn't tiny (detached
		// sessions default to 80x24). Errors are non-fatal.
		m.tmuxClient.ResizeToClient(sessionName)
	}
	if err != nil {
		return "", "", fmt.Errorf("create tmux session: %w", err)
	}

	// Send the start command to the new pane so aliases resolve.
	if command != "" {
		if err := m.tmuxClient.SendKeys(target, command); err != nil {
			return "", "", fmt.Errorf("send start command: %w", err)
		}
		if err := m.tmuxClient.SendKeys(target, "Enter"); err != nil {
			return "", "", fmt.Errorf("send enter: %w", err)
		}
	}

	// Extract the tmux session name from the target (e.g. "mysess:0.0" -> "mysess").
	parts := strings.SplitN(target, ":", 2)
	tmuxSession = parts[0]

	return tmuxSession, target, nil
}

// CreateNewWithTarget types command into a new window's shell so aliases and PATH
// apply, and Claude can't replace pane_current_command with its version string.
func (m *Manager) CreateNewWithTarget(cwd, targetTmuxSession, command, fallbackName string) (string, error) {
	if cwd == "" {
		cwd = "."
	}
	if command == "" {
		return "", fmt.Errorf("new session: empty start command")
	}
	target, err := m.tmuxClient.NewWindowPrint(tmux.NewWindowOptions{
		Target: targetTmuxSession + ":",
		Name:   m.nameWindow(filepath.Base(cwd), fallbackName),
	})
	if err != nil {
		return "", fmt.Errorf("create window: %w", err)
	}
	cmd := fmt.Sprintf("cd %s && %s", shellSingleQuote(cwd), command)
	if err := m.tmuxClient.SendKeys(target, cmd); err != nil {
		return "", fmt.Errorf("send start command: %w", err)
	}
	if err := m.tmuxClient.SendKeys(target, "Enter"); err != nil {
		return "", fmt.Errorf("send enter: %w", err)
	}
	return target, nil
}
