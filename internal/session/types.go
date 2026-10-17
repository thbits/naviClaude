package session

import "time"

// PaneSignal is the signal read from pane content alone; StatusTracker combines it
// with CPU and transcript activity.
type PaneSignal int

const (
	// SignalNone shows neither a prompt nor the "interrupt" footer; activity must
	// come from CPU/transcript signals.
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

// Background counts the subagents and background shells a session has running.
type Background struct {
	Agents int
	Shells int
}

// ChangedFile is a file the session edited, with line counts estimated from the
// transcript unless replaced by a live git diff (see GitDiffStats).
type ChangedFile struct {
	Path      string
	Added     int
	Removed   int
	Estimated bool // true when counts are the transcript estimate, not a live git diff
}
