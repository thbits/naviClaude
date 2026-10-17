package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/thbits/naviClaude/internal/session"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// nonAlphanumRe matches any character that is not a letter, digit, or dash.
var nonAlphanumRe = regexp.MustCompile(`[^a-zA-Z0-9-]`)

// cwdSlug mirrors Claude's ~/.claude/projects slug (non-alphanumerics but dash
// become dashes). It is lossy, but must match Claude's scheme byte for byte.
func cwdSlug(cwd string) string {
	return nonAlphanumRe.ReplaceAllString(cwd, "-")
}

// claudeHomeDir returns ~/.claude, shared so every reader agrees on the base path.
func claudeHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// LoadConversation returns a transcript's text turns oldest first, keeping only
// the last maxTurns so long sessions still show recent activity.
func LoadConversation(sess *session.Session, maxTurns int) ([]session.ConversationEntry, error) {
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

	var entries []session.ConversationEntry

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rec struct {
			Type        string          `json:"type"`
			Message     json.RawMessage `json:"message"`
			IsMeta      bool            `json:"isMeta"`
			IsSidechain bool            `json:"isSidechain"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}

		// Exclude injected/meta records and subagent traffic so the pane matches
		// what MessageCount counts (see session.isConversationalTurn).
		if rec.IsMeta || rec.IsSidechain {
			continue
		}
		if rec.Type != "user" && rec.Type != "assistant" {
			continue
		}

		text := extractMessageText(rec.Message)
		if text == "" {
			continue
		}

		entries = append(entries, session.ConversationEntry{
			Role: rec.Type,
			Text: text,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if maxTurns > 0 && len(entries) > maxTurns {
		entries = entries[len(entries)-maxTurns:]
	}

	return entries, nil
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

// SessionFilePath returns a session's expected .jsonl path without checking it
// exists; "" when sessionID, cwd or the home dir is unknown.
func SessionFilePath(sessionID, cwd string) string {
	if sessionID == "" || cwd == "" {
		return ""
	}
	base, err := claudeHomeDir()
	if err != nil {
		return ""
	}
	slug := cwdSlug(cwd)
	return filepath.Join(base, "projects", slug, sessionID+".jsonl")
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
