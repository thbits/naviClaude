package session

import (
	"bufio"
	"encoding/json"
	"os"
)

// lastAITitle returns the aiTitle of the most recent "ai-title" record in a
// session's .jsonl transcript. Claude Code appends a fresh ai-title record each
// time it (re)generates the session's title, so the physically-last one is the
// current title. Returns "" when the file is missing, has no ai-title records,
// or cannot be read. Malformed and oversized lines are skipped, matching the
// tolerance of the other transcript scanners (lastMessageTime, parseSessionFile).
func lastAITitle(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var title string
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxTranscriptLineBytes)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			AITitle string `json:"aiTitle"`
		}
		if err := json.Unmarshal(line, &rec); err != nil || rec.Type != "ai-title" {
			continue
		}
		if rec.AITitle != "" {
			title = rec.AITitle
		}
	}
	return title
}
