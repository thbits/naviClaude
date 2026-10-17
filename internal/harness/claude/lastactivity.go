package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// lastMessageTime returns a transcript's max record timestamp, scanning it all as
// order isn't chronological; unlike mtime, `claude --resume` doesn't bump it.
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
