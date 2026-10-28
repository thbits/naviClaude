package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
)

var _ harness.ActivityProvider = (*Harness)(nil)

// Transcripts reach tens of MB but the live turn sits at the end, so only the
// tail is parsed; the larger window is a retry when no turn start fits.
const (
	activityTailBytes    = 256 << 10
	activityTailMaxBytes = 1 << 20
)

// LoadActivity implements harness.ActivityProvider; nil with no error means
// there is no transcript to read.
func (h *Harness) LoadActivity(s *session.Session) (*session.SessionActivity, error) {
	path := transcriptPath(s)
	if path == "" {
		return nil, nil
	}
	var since time.Time
	if s.PID > 0 {
		since = lookupProc(s.PID).start
	}
	return loadActivity(path, since)
}

type activityCacheEntry struct {
	size  int64
	mod   time.Time
	since time.Time
	act   session.SessionActivity
}

var (
	activityMu    sync.Mutex
	activityCache = map[string]activityCacheEntry{}
)

// LoadActivity reports what the transcript at path is doing now from its tail,
// cached by size and mtime so polling an unchanged file costs one stat.
func LoadActivity(path string) (*session.SessionActivity, error) {
	return loadActivity(path, time.Time{})
}

// loadActivity is LoadActivity for a process started at since (zero if unknown),
// so background work from an earlier run of the session is not counted.
func loadActivity(path string, since time.Time) (*session.SessionActivity, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	activityMu.Lock()
	prev, hit := activityCache[path]
	activityMu.Unlock()
	if hit && prev.size == fi.Size() && prev.mod.Equal(fi.ModTime()) && prev.since.Equal(since) {
		act := prev.act
		act.Shells, act.Monitors = countBackground(path, since)
		return &act, nil
	}

	data, truncated, err := readActivityTail(path, activityTailBytes)
	if err != nil {
		return nil, err
	}
	act, sawTurn := parseActivity(data, truncated, since)
	if !sawTurn && truncated {
		// Large attachment and request records can crowd a short turn out.
		if more, trunc, err := readActivityTail(path, activityTailMaxBytes); err == nil {
			data, truncated = more, trunc
			act, sawTurn = parseActivity(data, truncated, since)
		}
	}
	// No turn start in the window means none happened since the last read, as
	// long as the file grew by less than the window; keep the earlier bound.
	if !sawTurn && hit && fi.Size() >= prev.size && fi.Size()-prev.size <= int64(len(data)) &&
		!prev.act.TurnStart.IsZero() && prev.act.TurnStart.Before(act.TurnStart) {
		act.TurnStart = prev.act.TurnStart
		act.TurnStartApprox = prev.act.TurnStartApprox
	}

	// A tail crowded by large records may hold no assistant model; the model
	// rarely changes, so keep the last one seen.
	if act.Model == "" && hit {
		act.Model = prev.act.Model
	}

	activityMu.Lock()
	activityCache[path] = activityCacheEntry{size: fi.Size(), mod: fi.ModTime(), since: since, act: act}
	activityMu.Unlock()
	act.Shells, act.Monitors = countBackground(path, since)
	return &act, nil
}

// readActivityTail returns up to the last n bytes of path, starting at a line
// boundary. truncated reports whether earlier bytes were skipped.
func readActivityTail(path string, n int64) (data []byte, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	off := fi.Size() - n
	if off < 0 {
		off = 0
	}
	data = make([]byte, fi.Size()-off)
	if _, err := f.ReadAt(data, off); err != nil && err != io.EOF {
		return nil, false, err
	}
	if off > 0 {
		// The first line is almost certainly cut mid-record.
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			data = nil
		}
	}
	return data, off > 0, nil
}

type activityRecord struct {
	Type          string          `json:"type"`
	Subtype       string          `json:"subtype"`
	Timestamp     string          `json:"timestamp"`
	IsMeta        bool            `json:"isMeta"`
	IsSidechain   bool            `json:"isSidechain"`
	Message       json.RawMessage `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Attachment    *struct {
		Type   string          `json:"type"`
		Prompt json.RawMessage `json:"prompt"`
	} `json:"attachment"`
	// Reported on turn_duration records: background agents still running.
	PendingBackgroundAgentCount *int `json:"pendingBackgroundAgentCount"`
}

type activityMessage struct {
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type activityBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Text      string          `json:"text"`
}

type pendingTool struct {
	id, name, target string
}

var notificationToolUseID = regexp.MustCompile(`<tool-use-id>([^<]+)</tool-use-id>`)

// parseActivity derives the main thread's current activity from transcript lines
// in file order. Tools and agents recorded before since are dropped; shells
// and monitors are counted separately by countBackground.
// sawTurn reports whether a turn start was in the data.
func parseActivity(data []byte, truncated bool, since time.Time) (act session.SessionActivity, sawTurn bool) {
	var (
		pending   []pendingTool
		async     = map[string]bool{} // background agents awaiting a completion notification
		turnOpen  bool
		answered  bool // an assistant record followed the turn's start
		earliest  time.Time
		tokensAt  time.Time
		modelAt   time.Time
		bgFloor   int  // pendingBackgroundAgentCount at the latest turn end
		bgDoneAft int  // completion notifications since that turn end
		priorRun  bool // records from before since are being tracked
	)
	// An earlier run's tools and agents died with its process (e.g. a
	// resumed session), so they are dropped once its records end.
	dropPriorRun := func() {
		if priorRun {
			turnOpen = false
			pending = nil
			clear(async)
			bgFloor, bgDoneAft = 0, 0
			priorRun = false
		}
	}
	startTurn := func(ts time.Time) {
		act.TurnStart = ts
		turnOpen = true
		answered = false
		sawTurn = true
		pending = nil // tools from an abandoned turn never get results
	}
	taskNotified := func(text string) {
		if m := notificationToolUseID.FindStringSubmatch(text); m != nil {
			delete(async, m[1])
		}
		// Shell and monitor notifications leave the agent floor alone.
		if !strings.Contains(text, "<summary>Background command") && !strings.Contains(text, "<summary>Monitor") {
			bgDoneAft++
		}
	}

	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec activityRecord
		if json.Unmarshal(line, &rec) != nil || rec.IsSidechain {
			continue
		}
		if rec.Type != "user" && rec.Type != "assistant" && rec.Type != "system" && rec.Type != "attachment" {
			continue
		}
		ts := parseTimestamp(rec.Timestamp)
		if !ts.IsZero() && (earliest.IsZero() || ts.Before(earliest)) {
			earliest = ts
		}
		if !since.IsZero() && !ts.IsZero() {
			if ts.Before(since) {
				priorRun = true
			} else {
				dropPriorRun()
			}
		}

		switch rec.Type {
		case "attachment":
			// A notification arriving mid-turn is recorded as a queued command, not a user record.
			var text string
			if a := rec.Attachment; a != nil && a.Type == "queued_command" && json.Unmarshal(a.Prompt, &text) == nil &&
				strings.HasPrefix(strings.TrimSpace(text), "<task-notification>") {
				taskNotified(text)
			}

		case "system":
			if rec.Subtype == "turn_duration" {
				turnOpen = false
				pending = nil
				// The count is omitted when zero.
				bgFloor, bgDoneAft = 0, 0
				if rec.PendingBackgroundAgentCount != nil {
					bgFloor = *rec.PendingBackgroundAgentCount
				}
			}

		case "assistant":
			var msg activityMessage
			if json.Unmarshal(rec.Message, &msg) != nil {
				continue
			}
			answered = true
			if ts.After(act.LastAssistant) {
				act.LastAssistant = ts
			}
			if msg.Model != "" && msg.Model != syntheticModel && !ts.Before(modelAt) {
				act.Model = msg.Model
				modelAt = ts
			}
			if u := msg.Usage; u != nil && msg.Model != syntheticModel && !ts.Before(tokensAt) {
				if total := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens + u.OutputTokens; total > 0 {
					act.TokensUsed = total
					tokensAt = ts
				}
			}
			var blocks []activityBlock
			if json.Unmarshal(msg.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				if b.Type == "tool_use" && b.ID != "" {
					name, target := toolSummary(b.Name, b.Input)
					pending = append(pending, pendingTool{id: b.ID, name: name, target: target})
				}
			}

		case "user":
			var msg activityMessage
			if json.Unmarshal(rec.Message, &msg) != nil {
				continue
			}
			text, results := userContent(msg.Content)
			launched, _ := asyncLaunch(rec.ToolUseResult)
			for _, id := range results {
				for i, p := range pending {
					if p.id != id {
						continue
					}
					if launched && isAgentTool(p.name) {
						async[id] = true
					}
					pending = append(pending[:i], pending[i+1:]...)
					break
				}
			}
			text = strings.TrimSpace(text)
			switch {
			case text == "":
			case strings.HasPrefix(text, "<task-notification>"):
				taskNotified(text)
				// Mid-turn notifications are folded into the running turn.
				if !turnOpen || !answered {
					startTurn(ts)
				}
			case strings.HasPrefix(text, "[Request interrupted by user"):
				turnOpen = false
				pending = nil
			case strings.HasPrefix(text, "<local-command-"), strings.HasPrefix(text, "Caveat:"):
			case rec.IsMeta:
				if !turnOpen || !answered {
					startTurn(ts)
				}
			default:
				startTurn(ts)
			}
		}
	}

	dropPriorRun()

	if !sawTurn {
		act.TurnStart = earliest
		act.TurnStartApprox = truncated
	}
	act.TurnEnded = !turnOpen
	if n := len(pending); n > 0 {
		act.Tool, act.Target = pending[n-1].name, pending[n-1].target
	}
	for _, p := range pending {
		if isAgentTool(p.name) {
			act.Agents++
		}
	}
	bg := len(async)
	if floor := bgFloor - bgDoneAft; floor > bg {
		bg = floor
	}
	act.Agents += bg
	return act, sawTurn
}

// userContent returns a user record's visible text and the tool_use ids its
// tool_result blocks answer.
func userContent(raw json.RawMessage) (text string, results []string) {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str, nil
	}
	var blocks []activityBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return "", nil
	}
	var sb strings.Builder
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			results = append(results, b.ToolUseID)
		case "text":
			sb.WriteString(b.Text)
		}
	}
	return sb.String(), results
}

// asyncLaunch reports whether a tool result launched a background agent, and a
// backgrounded shell's task id; both results arrive long before the work ends.
func asyncLaunch(raw json.RawMessage) (agent bool, shellTask string) {
	if len(raw) == 0 || raw[0] != '{' {
		return false, ""
	}
	var r struct {
		IsAsync          bool   `json:"isAsync"`
		BackgroundTaskID string `json:"backgroundTaskId"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return false, ""
	}
	return r.IsAsync, r.BackgroundTaskID
}

func isAgentTool(name string) bool { return name == "Agent" || name == "Task" }

func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

// bashCommandMax is the longest Bash command shown verbatim; longer commands
// fall back to the tool call's description.
const bashCommandMax = 40

// toolSummary returns the display name for a tool and the most informative
// short piece of its input.
func toolSummary(name string, raw json.RawMessage) (string, string) {
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		if i := strings.LastIndex(rest, "__"); i >= 0 {
			rest = rest[i+2:]
		}
		return "MCP", rest
	}
	var in map[string]any
	_ = json.Unmarshal(raw, &in)
	str := func(key string) string {
		v, _ := in[key].(string)
		return strings.TrimSpace(v)
	}
	switch name {
	case "Bash":
		cmd := firstLine(str("command"))
		if desc := str("description"); desc != "" && (cmd == "" || len([]rune(cmd)) > bashCommandMax) {
			return name, desc
		}
		return name, cmd
	case "Edit", "MultiEdit", "Write", "Read", "NotebookEdit":
		p := str("file_path")
		if p == "" {
			p = str("notebook_path")
		}
		if p == "" {
			return name, ""
		}
		return name, filepath.Base(p)
	case "Grep", "Glob":
		return name, str("pattern")
	case "Agent", "Task":
		return name, str("description")
	case "WebFetch":
		if u, err := url.Parse(str("url")); err == nil && u.Host != "" {
			return name, u.Host
		}
		return name, ""
	case "WebSearch":
		return name, str("query")
	case "Skill":
		return name, str("skill")
	case "AskUserQuestion":
		if qs, ok := in["questions"].([]any); ok && len(qs) > 0 {
			if q, ok := qs[0].(map[string]any); ok {
				v, _ := q["question"].(string)
				return name, firstLine(v)
			}
		}
		return name, ""
	}
	return name, ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
