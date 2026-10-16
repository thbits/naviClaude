package session

import "time"

// SessionStatus represents the lifecycle state of an agent session.
type SessionStatus int

const (
	StatusActive  SessionStatus = iota // agent is working
	StatusIdle                         // Process running but no recent activity
	StatusWaiting                      // Waiting for user input
	StatusClosed                       // Session has ended; data from .jsonl only
)

// String returns a human-readable label for the status.
func (s SessionStatus) String() string {
	switch s {
	case StatusActive:
		return "Active"
	case StatusIdle:
		return "Idle"
	case StatusWaiting:
		return "Waiting"
	case StatusClosed:
		return "Closed"
	default:
		return "Unknown"
	}
}

// Session holds all metadata about a single agent session, whether it is
// currently running in tmux or was discovered from the harness's history.
type Session struct {
	// ID is the UUID from the .jsonl filename, or derived from tmux pane info
	// for sessions whose file has not been found yet.
	ID string

	// TmuxSession is the parent tmux session name (the grouping key in the UI).
	TmuxSession string

	// TmuxTarget is the "session:window.pane" address used with tmux commands.
	// Empty for closed sessions.
	TmuxTarget string

	// CWD is the working directory when the session was started.
	CWD string

	// GitBranch is the git branch active at session start, read from .jsonl.
	GitBranch string

	// Status is one of StatusActive / StatusIdle / StatusWaiting / StatusClosed.
	Status SessionStatus

	// NativeStatus is true when Status came from the harness's own state file
	// rather than pane/CPU heuristics.
	NativeStatus bool

	// WaitingFor is the harness's reason while Waiting, e.g. "permission prompt".
	WaitingFor string

	// TmuxPaneID is the stable tmux pane id ("%12") for live sessions.
	TmuxPaneID string

	// Attention is the hook-set marker on the pane: "waiting", "done" or "".
	Attention string

	// LastActivity is the timestamp of the most recent record in the .jsonl file,
	// or the time the tmux pane was last seen active for live sessions.
	LastActivity time.Time

	// ProjectName is derived from filepath.Base(CWD).
	ProjectName string

	// Summary is the first user prompt text, sourced from history.jsonl display field.
	Summary string

	// CPU is the CPU percentage from ps (populated for active sessions only).
	CPU float64

	// Memory is the resident set size in MB from ps (active sessions only).
	Memory float64

	// SessionFile is the absolute path to the .jsonl file for this session.
	// Used when resuming a closed session.
	SessionFile string

	// PID is the process ID for active sessions.
	PID int

	// Slug is the memorable name stored in the .jsonl file (e.g. "replicated-purring-cocke").
	Slug string

	// Model is the model family: "opus", "sonnet", or "haiku".
	// Derived by matching the model ID string in assistant-type records.
	Model string

	// Version is the agent CLI version string (e.g. "2.1.73").
	Version string

	// DisplayName is a user-set override for the session title.
	// When non-empty it takes precedence over Slug and ProjectName in the sidebar.
	DisplayName string

	// Subprocess is true when the agent is running inside another program (e.g.
	// neovim's embedded terminal). The tmux pane belongs to the parent app,
	// so preview capture and pane resize are not meaningful.
	Subprocess bool

	// SubprocessParent is the name of the parent program (e.g. "nvim").
	SubprocessParent string

	// Harness names the agent CLI that owns this session (e.g. "claude").
	Harness string
}

// Key identifies a session across harnesses. Claude sessions keep their bare ID
// so existing alias and view-state files stay valid.
func (s *Session) Key() string {
	if s.Harness == "" || s.Harness == "claude" || s.ID == "" {
		return s.ID
	}
	return s.Harness + ":" + s.ID
}
