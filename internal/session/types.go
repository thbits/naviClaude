package session

import "time"

// PaneSignal is the input/activity signal extracted purely from a pane's
// captured content, independent of process or CPU state. The StatusTracker
// combines it with CPU and transcript signals to produce a final SessionStatus.
type PaneSignal int

const (
	// SignalNone means the content shows neither a waiting prompt nor the
	// "interrupt" footer; the session is idle, or its activity must be inferred
	// from CPU/transcript signals.
	SignalNone PaneSignal = iota
	// SignalWaiting means an interactive prompt requiring user input is visible.
	SignalWaiting
	// SignalWorking means the agent is generating or running tools.
	SignalWorking
)

// ConversationEntry is a single turn in the conversation.
type ConversationEntry struct {
	Role string // "user" or "assistant"
	Text string
}

// SessionMetrics holds aggregate statistics extracted from a session's .jsonl file.
type SessionMetrics struct {
	MessageCount int       // conversational turns (see isConversationalTurn): user/assistant records with visible text, excluding meta, sidechain, and tool-only records
	StartTime    time.Time // first non-zero timestamp in the file
	TokensUsed   int       // context fill (input + cache_read + cache_creation + output) from the assistant record with the max timestamp
	ContextLimit int       // inferred from the transcript's model id (see claude.ContextLimitForModel), raised if usage exceeds it
	ModelID      string    // full model id of the latest assistant record, e.g. "claude-opus-5-5" ("" when none)
}

// SessionActivity is what a session's main thread is doing now, read from its
// transcript tail; Status still decides working vs waiting vs idle.
type SessionActivity struct {
	Tool   string // latest tool_use still awaiting its result ("" when none)
	Target string // short summary of that tool's input, e.g. a file name

	// TurnStart is when the current turn began. When the turn started before
	// the tail window, TurnStartApprox is set and TurnStart is a lower bound.
	TurnStart       time.Time
	TurnStartApprox bool

	LastAssistant time.Time // timestamp of the latest assistant record
	TurnEnded     bool      // the latest turn finished; only background work may remain
	Agents        int       // subagents still running, foreground or background
	Shells        int       // background shell commands still running
	TokensUsed    int       // context fill from the latest assistant usage in the tail
	Model         string    // model id of the latest main-thread assistant record ("" when none in the tail)
}

// ChangedFile is a file the session edited, with the added/removed line counts
// accumulated across every edit the session made to it. As loaded here the
// counts are derived from the transcript (old_string vs new_string per edit) --
// a durable but approximate record of what the session changed. Callers may
// replace the counts with a live git diff (see GitDiffStats) and clear
// Estimated; when no live diff exists (e.g. the work was committed) the
// transcript counts remain as the persistent estimate.
type ChangedFile struct {
	Path      string
	Added     int
	Removed   int
	Estimated bool // true when counts are the transcript estimate, not a live git diff
}
