package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// nonAlphanumRe matches any character that is not a letter, digit, or dash.
var nonAlphanumRe = regexp.MustCompile(`[^a-zA-Z0-9-]`)

// cwdSlug converts a CWD path into the directory slug used by Claude Code
// under ~/.claude/projects/. Claude Code replaces all non-alphanumeric
// characters (except dashes) with dashes.
func cwdSlug(cwd string) string {
	return nonAlphanumRe.ReplaceAllString(cwd, "-")
}

// ConversationEntry is a single turn in the conversation.
type ConversationEntry struct {
	Role string // "user" or "assistant"
	Text string
}

// LoadConversation reads a session's .jsonl file and extracts the user/assistant
// conversation turns as displayable text. Returns at most maxTurns entries.
func LoadConversation(sess *Session, maxTurns int) ([]ConversationEntry, error) {
	sessionFile := sess.SessionFile
	if sessionFile == "" {
		// Derive the file path from session ID and CWD. Active sessions
		// detected by the Detector don't populate SessionFile.
		sessionFile = deriveSessionFilePath(sess.ID, sess.CWD)
	}
	if sessionFile == "" {
		return nil, fmt.Errorf("no session file for session %s", sess.ID)
	}

	f, err := os.Open(sessionFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []ConversationEntry

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rec struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}

		if rec.Type != "user" && rec.Type != "assistant" {
			continue
		}

		text := extractMessageText(rec.Message)
		if text == "" {
			continue
		}

		entries = append(entries, ConversationEntry{
			Role: rec.Type,
			Text: text,
		})

		if maxTurns > 0 && len(entries) >= maxTurns {
			break
		}
	}

	return entries, scanner.Err()
}

// extractMessageText pulls readable text from a message JSON payload.
// Handles both string content and structured content blocks.
func extractMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try as a message object with content field.
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || len(msg.Content) == 0 {
		return ""
	}

	// Content might be a plain string.
	var strContent string
	if err := json.Unmarshal(msg.Content, &strContent); err == nil {
		return strContent
	}

	// Content might be an array of content blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

// SessionFilePath constructs the .jsonl file path from a session ID and CWD,
// using the same slug convention as Claude Code: CWD with "/" replaced by "-".
// Returns empty string if sessionID or cwd is empty, or if home dir cannot be
// determined. Does NOT check file existence.
func SessionFilePath(sessionID, cwd string) string {
	if sessionID == "" || cwd == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	slug := cwdSlug(cwd)
	return filepath.Join(home, ".claude", "projects", slug, sessionID+".jsonl")
}

// deriveSessionFilePath is like SessionFilePath but also verifies the file
// exists (returning empty string if not).
func deriveSessionFilePath(sessionID, cwd string) string {
	path := SessionFilePath(sessionID, cwd)
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
