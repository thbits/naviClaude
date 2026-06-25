package session

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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

// shellSingleQuote returns s wrapped so it is safe to embed in a POSIX shell
// command as a single literal argument. Go's %q double-quotes, which still lets
// the shell expand $, backticks, and backslash escapes inside the result -- a
// directory path containing those would be misinterpreted (or used for command
// injection). Single quotes suppress all shell expansion; the only character
// that cannot appear inside single quotes is the single quote itself, which is
// emitted as the standard close-escape-reopen sequence '\” (close quote,
// backslash-escaped quote, reopen quote). Use this for filesystem paths only;
// the user-configured claude command is intentionally left shell-evaluated.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
	cmd := resumeShellCommand(sess.CWD, sess.ID)
	if fork {
		cmd += " --fork-session"
	}
	return m.tmuxClient.NewWindow(tmux.NewWindowOptions{
		Target:  targetTmuxSession + ":",
		Name:    resumeWindowName(sess),
		Command: cmd,
	})
}

// resumeShellCommand builds the shell command that resumes a closed session:
// it cd's into the session's working directory (defaulting to ".") and runs
// `claude --resume "<id>"`. The cwd is single-quoted to suppress shell
// expansion; the id is double-quoted via %q. This is the single source of the
// resume command shared by the new-window and new-session resume paths.
func resumeShellCommand(cwd, id string) string {
	if cwd == "" {
		cwd = "."
	}
	return fmt.Sprintf("cd %s && claude --resume %q", shellSingleQuote(cwd), id)
}

// resumeWindowName is the tmux window/session name to use for a resumed
// session: the project name, falling back to "claude".
func resumeWindowName(sess *Session) string {
	if sess.ProjectName != "" {
		return sess.ProjectName
	}
	return "claude"
}

// ResumeInSession resumes the closed session in the tmux session named
// targetName, creating that session if it does not yet exist. When targetName
// already exists the resumed Claude opens in a new window within it (identical
// to Resume); otherwise a new detached tmux session named targetName is created
// rooted at the session's working directory, sized to the current client, and
// the resume command is sent to its pane via send-keys (so the user's shell
// config -- aliases, PATH -- is respected, matching CreateNewTmuxSession).
func (m *Manager) ResumeInSession(sess *Session, targetName string) error {
	if sess.ID == "" {
		return fmt.Errorf("resume: session has no ID")
	}
	if targetName == "" {
		return fmt.Errorf("resume: target session name is empty")
	}
	if m.tmuxClient.HasSession(targetName) {
		return m.Resume(sess, targetName)
	}

	cwd := sess.CWD
	if cwd == "" {
		cwd = "."
	}
	target, err := m.tmuxClient.NewSessionPrint(tmux.NewSessionOptions{
		Name:       targetName,
		WindowName: resumeWindowName(sess),
		StartDir:   cwd,
	})
	if err != nil {
		return fmt.Errorf("create tmux session: %w", err)
	}
	// Resize to match the current client (detached sessions default to 80x24).
	// Errors are non-fatal.
	m.tmuxClient.ResizeToClient(targetName)

	cmd := resumeShellCommand(cwd, sess.ID)
	if err := m.tmuxClient.SendKeys(target, cmd); err != nil {
		return fmt.Errorf("send resume command: %w", err)
	}
	if err := m.tmuxClient.SendKeys(target, "Enter"); err != nil {
		return fmt.Errorf("send enter: %w", err)
	}
	return nil
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

// CreateNewTmuxSession creates a brand new tmux session (detached), sends the
// given command to start Claude, and returns the pane target. The command is
// sent via send-keys so that shell aliases and PATH are respected.
// If sessionName is empty, the directory basename is used as the tmux session name.
func (m *Manager) CreateNewTmuxSession(cwd, claudeCmd, sessionName string) (tmuxSession, target string, err error) {
	if cwd == "" {
		cwd = "."
	}
	windowName := filepath.Base(cwd)
	if windowName == "" || windowName == "." || windowName == "/" {
		windowName = "claude"
	}
	if sessionName == "" {
		sessionName = windowName
	}

	// If a tmux session with this name already exists, open Claude in a new
	// window within it instead of failing with tmux's "duplicate session"
	// error. This makes "N" reuse an existing session by name rather than only
	// ever creating a brand new one.
	if m.tmuxClient.HasSession(sessionName) {
		target, err = m.CreateNewWithTarget(cwd, sessionName, claudeCmd)
		if err != nil {
			return "", "", err
		}
		return sessionName, target, nil
	}

	target, err = m.tmuxClient.NewSessionPrint(tmux.NewSessionOptions{
		Name:       sessionName,
		WindowName: windowName,
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

	// Send the claude command to the new pane so aliases resolve.
	if claudeCmd != "" {
		if err := m.tmuxClient.SendKeys(target, claudeCmd); err != nil {
			return "", "", fmt.Errorf("send claude command: %w", err)
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

// CreateNewWithTarget opens a new tmux window in targetTmuxSession, changes to
// cwd, starts Claude using claudeCmd, and returns the new pane's tmux target
// (e.g. "session:3.0"). claudeCmd defaults to "claude" when empty.
//
// The command is typed into the pane's shell via send-keys rather than passed to
// new-window, so the user's interactive shell config -- aliases (e.g. a "cc"
// alias), functions, and PATH -- is respected, matching how CreateNewTmuxSession
// (the "N" key) launches Claude. This also makes Claude run as a child of the
// pane's shell, which the detector recognizes: a window command instead execs
// Claude as the pane's own process, whose tmux pane_current_command is the
// version string (e.g. "2.1.191"), not "claude".
func (m *Manager) CreateNewWithTarget(cwd, targetTmuxSession, claudeCmd string) (string, error) {
	if cwd == "" {
		cwd = "."
	}
	if claudeCmd == "" {
		claudeCmd = "claude"
	}
	name := filepath.Base(cwd)
	if name == "" || name == "." {
		name = "claude"
	}
	target, err := m.tmuxClient.NewWindowPrint(tmux.NewWindowOptions{
		Target: targetTmuxSession + ":",
		Name:   name,
	})
	if err != nil {
		return "", fmt.Errorf("create window: %w", err)
	}
	cmd := fmt.Sprintf("cd %s && %s", shellSingleQuote(cwd), claudeCmd)
	if err := m.tmuxClient.SendKeys(target, cmd); err != nil {
		return "", fmt.Errorf("send claude command: %w", err)
	}
	if err := m.tmuxClient.SendKeys(target, "Enter"); err != nil {
		return "", fmt.Errorf("send enter: %w", err)
	}
	return target, nil
}
