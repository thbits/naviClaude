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
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
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

		if rec.Type == "user" || rec.Type == "assistant" {
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
