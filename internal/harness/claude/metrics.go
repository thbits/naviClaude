package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

// metricsRecord captures the fields we need from each .jsonl line for metrics.
type metricsRecord struct {
	Type        string          `json:"type"`
	Timestamp   string          `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
	IsMeta      bool            `json:"isMeta"`
	IsSidechain bool            `json:"isSidechain"`
}

// contentBlock captures the discriminator of a single message content block.
type contentBlock struct {
	Type string `json:"type"`
}

// isConversationalTurn reports whether a record represents an actual
// conversational message rather than tool plumbing or injected context.
//
// Excluded: injected/meta records (isMeta), subagent traffic (isSidechain),
// user records that are only tool_result blocks, and assistant records that
// carry no visible text (thinking-only or tool_use-only).
func isConversationalTurn(rec metricsRecord) bool {
	if rec.IsMeta || rec.IsSidechain {
		return false
	}
	// Only user/assistant records are conversational. This guard must precede
	// the content checks below: a non-conversation record (e.g. a "system"
	// record) can also carry plain-string content, and the string fast paths
	// would otherwise count it as a turn.
	if rec.Type != "user" && rec.Type != "assistant" {
		return false
	}

	// A plain string content is a real message (older/simple transcript form).
	var str string
	if err := json.Unmarshal(rec.Message, &str); err == nil {
		return strings.TrimSpace(str) != ""
	}

	var wrapper struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rec.Message, &wrapper); err != nil {
		return false
	}
	if err := json.Unmarshal(wrapper.Content, &str); err == nil {
		return strings.TrimSpace(str) != ""
	}

	var blocks []contentBlock
	if err := json.Unmarshal(wrapper.Content, &blocks); err != nil {
		return false
	}

	switch rec.Type {
	case "user":
		// Real human input has at least one non-tool_result block.
		for _, b := range blocks {
			if b.Type != "tool_result" {
				return true
			}
		}
		return false
	case "assistant":
		// A visible reply has at least one text block; thinking/tool_use alone
		// is not a message.
		for _, b := range blocks {
			if b.Type == "text" {
				return true
			}
		}
		return false
	}
	return false
}

// metricsUsage captures the model id and token counts of an assistant message.
type metricsUsage struct {
	Model string `json:"model"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// LoadMetrics reads a Claude JSONL session file and extracts aggregate metrics.
// The context limit comes from the latest assistant record's model id; model
// (a family like "opus" or a full id) is the fallback when none is recorded.
func LoadMetrics(filePath, model string) (*session.SessionMetrics, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &session.SessionMetrics{}
	// Full model id of the chosen usage record; the family-only param can't
	// tell a 1M Claude 5 model from a 200K legacy one.
	var modelID string

	// Track the assistant record carrying the LATEST timestamp; its usage is
	// the current context fill. File order is not guaranteed to be
	// chronological, so we select by max timestamp rather than physical last.
	var (
		tokensTime     time.Time // timestamp of the chosen assistant record
		tokensHaveTime bool      // whether the chosen record had a parseable timestamp
		tokensSeen     bool      // whether any assistant usage record has been chosen yet
	)

	handle := func(line []byte) {
		var rec metricsRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return
		}

		var recTime time.Time
		if rec.Timestamp != "" {
			recTime, _ = time.Parse(time.RFC3339Nano, rec.Timestamp)
			if recTime.IsZero() {
				recTime, _ = time.Parse(time.RFC3339, rec.Timestamp)
			}
		}

		if !recTime.IsZero() && m.StartTime.IsZero() {
			m.StartTime = recTime
		}

		if isConversationalTurn(rec) {
			m.MessageCount++
		}

		// Track the latest assistant record's context usage (not cumulative).
		// Current context fill = input + cache_read + cache_creation + output
		// from the most recent request, selected by MAX timestamp because file
		// order is not guaranteed to be chronological.
		if rec.Type == "assistant" && len(rec.Message) > 0 {
			var usage metricsUsage
			if err := json.Unmarshal(rec.Message, &usage); err == nil {
				// Decide whether this record is "later" than the chosen one.
				// A record with a parseable timestamp beats one without; among
				// timestamped records the larger timestamp wins. Untimestamped
				// records only win when nothing has been chosen yet (preserving
				// behavior for files lacking timestamps).
				newer := false
				switch {
				case !tokensSeen:
					newer = true
				case !recTime.IsZero() && (!tokensHaveTime || recTime.After(tokensTime)):
					newer = true
				case recTime.IsZero() && !tokensHaveTime:
					// Both untimestamped: keep last-seen ordering as before.
					newer = true
				}
				if newer {
					u := usage.Usage
					m.TokensUsed = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens + u.OutputTokens
					tokensSeen = true
					tokensTime = recTime
					tokensHaveTime = !recTime.IsZero()
					if usage.Model != "" && usage.Model != syntheticModel {
						modelID = usage.Model
					}
				}
			}
		}
	}

	// Lines can exceed any fixed buffer (inlined images reach 15MB+), so read
	// each one whole rather than stopping at the first oversized line.
	r := bufio.NewReaderSize(f, 256*1024)
	var long []byte
	var readErr error
	for {
		chunk, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			long = append(long, chunk...)
			continue
		}
		line := chunk
		if long != nil {
			line = append(long, chunk...)
			long = nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			readErr = err
			break
		}
		if line = bytes.TrimSpace(line); len(line) > 0 {
			handle(line)
		}
		if err != nil {
			break
		}
	}

	m.ModelID = modelID
	m.ContextLimit = resolveContextLimit(modelID, model, m.TokensUsed)
	return m, readErr
}

// syntheticModel marks locally generated assistant records (e.g. API errors)
// that carry no real model.
const syntheticModel = "<synthetic>"

// contextTiers are the known context window sizes, ascending.
var contextTiers = []int{200_000, 1_000_000}

// modelIDPattern captures family and major version from ids like
// "claude-haiku-5-5" or "claude-sonnet-5"; legacy "claude-3-5-sonnet" won't match.
var modelIDPattern = regexp.MustCompile(`^claude-([a-z]+)-(\d+)`)

// resolveContextLimit picks the limit for the observed model id (falling back
// to the caller's model), bumped to a larger tier if usage already exceeds it.
func resolveContextLimit(modelID, fallback string, tokensUsed int) int {
	if modelID == "" {
		modelID = fallback
	}
	limit := ContextLimitForModel(modelID)
	if tokensUsed <= limit {
		return limit
	}
	for _, tier := range contextTiers {
		if tier >= tokensUsed {
			return tier
		}
	}
	return contextTiers[len(contextTiers)-1]
}

// ContextLimitForModel returns the context window for a full model id (e.g.
// "claude-opus-5-5", "claude-sonnet-4-5[1m]") or a bare family name.
func ContextLimitForModel(model string) int {
	lower := strings.ToLower(strings.TrimSpace(model))
	if strings.HasSuffix(lower, "[1m]") {
		return 1_000_000
	}
	if sm := modelIDPattern.FindStringSubmatch(lower); sm != nil {
		if sm[1] == "fable" {
			return 1_000_000
		}
		if major, err := strconv.Atoi(sm[2]); err == nil && major >= 5 {
			return 1_000_000
		}
	}
	// Legacy ids and bare families: only Opus had a 1M window.
	if strings.Contains(lower, "opus") {
		return 1_000_000
	}
	return 200_000
}
