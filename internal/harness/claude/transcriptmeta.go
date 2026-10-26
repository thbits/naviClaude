package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// transcriptMeta is what Enrich reads from a transcript: the max record
// timestamp and the latest ai-title, plus the state to resume after an append.
type transcriptMeta struct {
	size  int64
	mtime time.Time
	// offset is just past the last parsed '\n'; tail hashes the bytes before it.
	offset  int64
	tail    uint64
	lastTS  time.Time
	aiTitle string
}

// metaKey includes the path because SessionFilePath derives from cwd, so one
// session ID can name different files.
type metaKey struct {
	id, path string
}

// scanTranscriptMeta reads path, resuming from prev when the file has only been
// appended to since; prev may be nil for a full scan.
func scanTranscriptMeta(path string, prev *transcriptMeta) (*transcriptMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	m := &transcriptMeta{}
	if prev != nil && info.Size() >= prev.offset && metaTailMatches(f, prev) {
		cp := *prev
		m = &cp
	}

	// Read only up to the stat'd size so a write landing mid-scan changes size
	// and triggers another incremental pass.
	r := bufio.NewReaderSize(io.NewSectionReader(f, m.offset, info.Size()-m.offset), 64*1024)
	var long []byte
	var n int64 // raw bytes of the current line so far
	for {
		chunk, err := r.ReadSlice('\n')
		n += int64(len(chunk))
		oversized := n > maxTranscriptLineBytes
		if errors.Is(err, bufio.ErrBufferFull) {
			if oversized {
				long = nil
			} else {
				long = append(long, chunk...)
			}
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		line := chunk
		if long != nil {
			line = append(long, chunk...)
			long = nil
		}
		if len(chunk) == 0 || chunk[len(chunk)-1] != '\n' {
			// A newline-less tail is parsed if already valid but left unconsumed,
			// so a half-written record is read whole once it completes.
			if !oversized && n > 0 && json.Valid(line) {
				m.parseLine(line)
			}
			break
		}
		if !oversized {
			m.parseLine(line)
		}
		m.offset += n
		n = 0
	}

	tail, err := readTail(f, m.offset)
	if err != nil {
		return nil, err
	}
	m.tail = hashBytes(tail)
	m.size = info.Size()
	m.mtime = info.ModTime()
	return m, nil
}

func metaTailMatches(f *os.File, prev *transcriptMeta) bool {
	tail, err := readTail(f, prev.offset)
	return err == nil && int64(len(tail)) == min(prev.offset, tailLen) && hashBytes(tail) == prev.tail
}

func (m *transcriptMeta) parseLine(line []byte) {
	var rec struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		AITitle   string `json:"aiTitle"`
	}
	if json.Unmarshal(line, &rec) != nil {
		return
	}
	if rec.Type == "ai-title" && rec.AITitle != "" {
		m.aiTitle = rec.AITitle
	}
	if rec.Timestamp == "" {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, rec.Timestamp); err != nil {
			return
		}
	}
	if t.After(m.lastTS) {
		m.lastTS = t
	}
}
