package session

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// lastMessageTime returns the timestamp of the most recent record in a
// session's .jsonl transcript: the MAXIMUM parseable timestamp across all
// records. It scans the whole file because record order is not guaranteed to
// be chronological, and the physically-last line is often an untimestamped
// meta record (custom-title, mode, file-history-snapshot). Returns the zero
// time when the file is missing, empty, or has no timestamped records.
//
// This is the "last activity" the UI wants -- unlike the file's mtime, which
// `claude --resume` bumps to now even when the last real message is hours old.
func lastMessageTime(path string) time.Time {
	if path == "" {
		return time.Time{}
	}
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()

	var last time.Time
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxTranscriptLineBytes)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &rec); err != nil || rec.Timestamp == "" {
			continue
		}
		t, perr := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if perr != nil {
			t, perr = time.Parse(time.RFC3339, rec.Timestamp)
			if perr != nil {
				continue
			}
		}
		if t.After(last) {
			last = t
		}
	}
	return last
}
