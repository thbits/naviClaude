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

// Manager performs tmux lifecycle operations on agent sessions: creating new
// ones, resuming closed ones, and killing running ones. Harnesses supply the
// shell commands.
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

// shellSingleQuote returns s wrapped so it is safe to embed in a POSIX shell
// command as a single literal argument. Go's %q double-quotes, which still lets
// the shell expand $, backticks, and backslash escapes inside the result -- a
// directory path containing those would be misinterpreted (or used for command
// injection). Single quotes suppress all shell expansion; the only character
// that cannot appear inside single quotes is the single quote itself, which is
// emitted as the standard close-escape-reopen sequence '\” (close quote,
// backslash-escaped quote, reopen quote). Use this for filesystem paths only;
// the user-configured start command is intentionally left shell-evaluated.
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

// ResumeInSession resumes the closed session in the tmux session named
// targetName, creating that session if it does not yet exist, and returns the
// tmux target of the resumed pane (e.g. "work:3.0") so the caller can focus it.
// When targetName already exists the resumed agent opens in a new window
// within it; otherwise a new detached tmux session named targetName is created
// rooted at the session's working directory, sized to the current client, and
// the resume command is sent to its pane via send-keys (so the user's shell
// config -- aliases, PATH -- is respected, matching CreateNewTmuxSession).
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

// Kill quits an active session's agent with SIGTERM so its SessionEnd hooks
// run, waits for it to exit, then closes its tmux pane. A pane owned by a
// parent app (e.g. nvim) is left open.
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

// CreateNewTmuxSession creates a brand new tmux session (detached), sends
// command to start the agent, and returns the pane target. The command is
// sent via send-keys so that shell aliases and PATH are respected.
// If sessionName is empty, the directory basename is used as the tmux session
// name; fallbackName is used when the basename is unusable.
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

	// If a tmux session with this name already exists, open the agent in a new
	// window within it instead of failing with tmux's "duplicate session"
	// error. This makes "N" reuse an existing session by name rather than only
	// ever creating a brand new one.
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

// CreateNewWithTarget opens a new tmux window in targetTmuxSession, changes to
// cwd, runs command, and returns the new pane's tmux target (e.g. "session:3.0").
// fallbackName names the window when cwd has no usable basename.
//
// The command is typed into the pane's shell via send-keys rather than passed to
// new-window, so the user's shell config (aliases, functions, PATH) applies, and
// the agent runs as a child of the shell. A window command would instead exec
// the agent as the pane's own process, which some CLIs retitle (e.g. Claude
// reports its version string as pane_current_command).
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
