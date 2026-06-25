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
//
// KNOWN AMBIGUITY: this mapping is non-injective. Distinct CWDs can collapse to
// the same slug (e.g. "/a/b" and "/a-b" both become "-a-b"; "/a_b" and "/a/b"
// both become "-a-b"). We intentionally do NOT change the output: it must match
// Claude Code's own on-disk slug scheme so that SessionFilePath can locate the
// .jsonl file under ~/.claude/projects/<slug>/. SessionFilePath relies on this
// scheme staying byte-for-byte identical to Claude's; if Claude changes its
// slugging, this must change in lockstep. Do not "improve" the encoding here.
func cwdSlug(cwd string) string {
	return nonAlphanumRe.ReplaceAllString(cwd, "-")
}

// claudeHomeDir returns the ~/.claude base directory, or an error if the user
// home directory cannot be determined. Centralizes the UserHomeDir +
// Join(".claude") computation so history.go and conversation.go agree on the
// base path. (Named to avoid colliding with helpers in detector.go.)
func claudeHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// ConversationEntry is a single turn in the conversation.
type ConversationEntry struct {
	Role string // "user" or "assistant"
	Text string
}

// LoadConversation reads a session's .jsonl file and extracts the user/assistant
// conversation turns as displayable text. Returns at most maxTurns entries; when
// the session has more than maxTurns text-bearing turns, the LAST maxTurns are
// returned (the most recent activity), preserving chronological order
// (oldest-to-newest) within that tail.
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
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Keep only the LAST maxTurns text-bearing turns so long sessions still
	// surface recent activity. Order is preserved (oldest-to-newest) within
	// the returned tail.
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

// SessionFilePath constructs the .jsonl file path from a session ID and CWD,
// using the same slug convention as Claude Code: CWD with "/" replaced by "-".
// Returns empty string if sessionID or cwd is empty, or if home dir cannot be
// determined. Does NOT check file existence.
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
