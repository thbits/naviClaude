package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"os"
	"strings"
	"time"
)

// Model families, stored as an index so cached events stay small.
var modelFamilies = []string{"opus", "sonnet", "haiku", "fable", "other"}

func modelFamily(model string) uint8 {
	lower := strings.ToLower(model)
	for i, f := range modelFamilies[:len(modelFamilies)-1] {
		if strings.Contains(lower, f) {
			return uint8(i)
		}
	}
	return uint8(len(modelFamilies) - 1)
}

// promptEvent is one prompt the user sent; ID hashes the record uuid, which
// forked sessions copy verbatim, so it dedupes across files.
type promptEvent struct {
	ID uint64
	TS int64 // unix seconds
}

// usageEvent is one API response; Claude writes a record per content block,
// all carrying the same usage, so ID (message id + request id) dedupes them.
type usageEvent struct {
	ID         uint64
	TS         int64
	Family     uint8
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// transcriptSummary is everything the stats need from one .jsonl file.
type transcriptSummary struct {
	Size    int64
	ModTime int64 // unix nanoseconds
	// Offset is the parsed prefix (a line boundary); Tail hashes the bytes before it,
	// so an appended file resumes from Offset while a rewritten one is rescanned.
	Offset  int64
	Tail    uint64
	Main    bool
	Cwd     string
	Prompts []promptEvent
	Usage   []usageEvent
}

const tailLen = 256

// transcriptRecord holds the few fields read from a user/assistant record.
type transcriptRecord struct {
	Type             string `json:"type"`
	UUID             string `json:"uuid"`
	Timestamp        string `json:"timestamp"`
	Cwd              string `json:"cwd"`
	RequestID        string `json:"requestId"`
	IsSidechain      bool   `json:"isSidechain"`
	IsMeta           bool   `json:"isMeta"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	IsAPIError       bool   `json:"isApiErrorMessage"`
	Origin           *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Message struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

var (
	typeUser         = [][]byte{[]byte(`"type":"user"`), []byte(`"type": "user"`)}
	typeAssistant    = [][]byte{[]byte(`"type":"assistant"`), []byte(`"type": "assistant"`)}
	toolUseResultKey = [][]byte{[]byte(`"toolUseResult":`), []byte(`"toolUseResult" :`)}
)

func containsAny(line []byte, pats [][]byte) bool {
	for _, p := range pats {
		if bytes.Contains(line, p) {
			return true
		}
	}
	return false
}

func hashString(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func hashBytes(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

// scanTranscript parses path, continuing from prev when the file has only been
// appended to since prev was recorded. prev may be nil.
func scanTranscript(path string, main bool, info os.FileInfo, prev *transcriptSummary) (*transcriptSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sum := &transcriptSummary{Main: main}
	if prev != nil && prev.Main == main && info.Size() >= prev.Offset && tailMatches(f, prev) {
		cp := *prev
		cp.Prompts = append([]promptEvent(nil), prev.Prompts...)
		cp.Usage = append([]usageEvent(nil), prev.Usage...)
		sum = &cp
	}
	if _, err := f.Seek(sum.Offset, io.SeekStart); err != nil {
		return nil, err
	}

	r := bufio.NewReaderSize(f, 256*1024)
	var long []byte
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
			return nil, err
		}
		complete := len(line) > 0 && line[len(line)-1] == '\n'
		// An unterminated last line is either mid-write (left for the next scan)
		// or a final record without a newline, which is valid JSON.
		if !complete && (len(line) == 0 || !json.Valid(bytes.TrimSpace(line))) {
			break
		}
		sum.parseLine(line)
		sum.Offset += int64(len(line))
		if err != nil {
			break
		}
	}

	// Size and ModTime come from the stat taken before reading, so a write that
	// lands mid-scan still changes them and triggers another (incremental) pass.
	tail, err := readTail(f, sum.Offset)
	if err != nil {
		return nil, err
	}
	sum.Tail = hashBytes(tail)
	sum.Size = info.Size()
	sum.ModTime = info.ModTime().UnixNano()
	return sum, nil
}

func readTail(f *os.File, offset int64) ([]byte, error) {
	start := offset - tailLen
	if start < 0 {
		start = 0
	}
	buf := make([]byte, offset-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf, nil
}

func tailMatches(f *os.File, prev *transcriptSummary) bool {
	tail, err := readTail(f, prev.Offset)
	return err == nil && int64(len(tail)) == min(prev.Offset, tailLen) && hashBytes(tail) == prev.Tail
}

func (s *transcriptSummary) parseLine(line []byte) {
	isUser := containsAny(line, typeUser)
	isAssistant := containsAny(line, typeAssistant)
	if !isUser && !isAssistant {
		return
	}
	// Tool results are never prompts and are the bulk of transcript bytes.
	if !isAssistant && containsAny(line, toolUseResultKey) {
		return
	}
	var rec transcriptRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return
	}
	if s.Cwd == "" && rec.Cwd != "" {
		s.Cwd = rec.Cwd
	}
	ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		return
	}
	switch rec.Type {
	case "user":
		if s.Main && isUserPrompt(&rec) {
			s.Prompts = append(s.Prompts, promptEvent{ID: hashString(rec.UUID), TS: ts.Unix()})
		}
	case "assistant":
		u := rec.Message.Usage
		if u == nil || rec.IsAPIError || rec.Message.Model == "<synthetic>" {
			return
		}
		if u.Input+u.Output+u.CacheRead+u.CacheWrite == 0 {
			return
		}
		key := rec.Message.ID + "|" + rec.RequestID
		if rec.Message.ID == "" {
			key = "uuid|" + rec.UUID
		}
		s.Usage = append(s.Usage, usageEvent{
			ID: hashString(key), TS: ts.Unix(), Family: modelFamily(rec.Message.Model),
			Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite,
		})
	}
}

// isUserPrompt reports whether a user record is a prompt the person sent, as
// opposed to tool output, slash-command echoes, notifications or meta records.
func isUserPrompt(rec *transcriptRecord) bool {
	if rec.IsSidechain || rec.IsMeta || rec.IsCompactSummary || rec.UUID == "" {
		return false
	}
	if rec.Origin != nil {
		return rec.Origin.Kind == "human"
	}
	// Older transcripts lack origin: accept plain text that isn't a command
	// echo (<command-…>, <bash-…>), an interrupt marker or a slash command.
	text, ok := promptText(rec.Message.Content)
	if !ok {
		return false
	}
	text = strings.TrimSpace(text)
	return text != "" && !strings.HasPrefix(text, "<") && !strings.HasPrefix(text, "[") && !strings.HasPrefix(text, "/")
}

// promptText returns the text of a user message, or false if it carries tool
// results.
func promptText(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", false
	}
	var b strings.Builder
	for _, bl := range blocks {
		switch bl.Type {
		case "tool_result":
			return "", false
		case "text":
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(bl.Text)
		}
	}
	return b.String(), true
}
