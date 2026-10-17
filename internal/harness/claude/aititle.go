package claude

import (
	"bufio"
	"encoding/json"
	"os"
)

// lastAITitle returns the last "ai-title" record's title, the current one since
// Claude appends a record per regeneration; "" when there is none or no file.
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
