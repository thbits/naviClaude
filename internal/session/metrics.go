package session

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// SessionMetrics holds aggregate statistics extracted from a session's .jsonl file.
type SessionMetrics struct {
	MessageCount   int       // count of type=="user" || type=="assistant" records
	StartTime      time.Time // first non-zero timestamp in the file
	TokensUsed     int       // context fill (input + cache_read + cache_creation + output) from the assistant record with the max timestamp
	ContextLimit   int       // inferred from model: opus->1000000, sonnet->200000, haiku->200000, default->200000
	RecentActivity [10]int   // message counts bucketed into 10 equal slots spanning the session's first-to-last message (adaptive window)
}

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

// metricsUsage captures token counts nested inside an assistant message.
type metricsUsage struct {
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// LoadMetrics reads a Claude JSONL session file and extracts aggregate metrics.
// The model parameter should be a classified model family string (e.g. "opus",
// "sonnet", "haiku") used to determine context limits.
func LoadMetrics(filePath, model string) (*SessionMetrics, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &SessionMetrics{
		ContextLimit: ContextLimitForModel(model),
	}

	// Collect all message timestamps in first pass, count tokens along the way.
	var msgTimes []time.Time

	// Track the assistant record carrying the LATEST timestamp; its usage is
	// the current context fill. File order is not guaranteed to be
	// chronological, so we select by max timestamp rather than physical last.
	var (
		tokensTime     time.Time // timestamp of the chosen assistant record
		tokensHaveTime bool      // whether the chosen record had a parseable timestamp
		tokensSeen     bool      // whether any assistant usage record has been chosen yet
	)

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rec metricsRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
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
			if !recTime.IsZero() {
				msgTimes = append(msgTimes, recTime)
			}
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
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return m, err
	}

	// Bucket messages across the session's lifetime into 10 slots.
	// Uses the full span from first to last message (adaptive window).
	if len(msgTimes) >= 2 {
		first := msgTimes[0]
		last := msgTimes[len(msgTimes)-1]
		span := last.Sub(first)
		if span < time.Minute {
			span = time.Minute // avoid division by zero for very short sessions
		}
		bucketDuration := span / 10

		for _, t := range msgTimes {
			bucket := int(t.Sub(first) / bucketDuration)
			if bucket < 0 {
				bucket = 0
			}
			if bucket >= 10 {
				bucket = 9
			}
			m.RecentActivity[bucket]++
		}
	} else if len(msgTimes) == 1 {
		m.RecentActivity[9] = 1 // single message goes in the last bucket
	}

	return m, nil
}

// ContextLimitForModel returns the context window size for a given model family.
// Opus models get 1,000,000 tokens; all others default to 200,000.
func ContextLimitForModel(model string) int {
	switch strings.ToLower(model) {
	case "opus":
		return 1_000_000
	case "sonnet":
		return 200_000
	case "haiku":
		return 200_000
	default:
		return 200_000
	}
}
