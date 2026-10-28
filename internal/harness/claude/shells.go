package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// A background shell often outlives the transcript tail parseActivity reads, and
// a subagent's shells are logged in its own transcript, so shells are tallied
// from every transcript of the session, each read once and then incrementally.

type shellLedger struct {
	size     int64 // bytes consumed, ending at a line boundary
	since    time.Time
	launched map[string]string // shell task id -> its tool_use id
	ended    map[string]bool   // task and tool_use ids that completed or were stopped
}

var (
	shellMu      sync.Mutex
	shellLedgers = map[string]*shellLedger{}
)

var notificationTaskID = regexp.MustCompile(`<task-id>([^<]+)</task-id>`)

// countShells returns the background shells still running in the session whose
// main transcript is path, counting only those launched at or after since.
func countShells(path string, since time.Time) int {
	files := []string{path}
	dir := filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents")
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if name := e.Name(); strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl") {
				files = append(files, filepath.Join(dir, name))
			}
		}
	}

	shellMu.Lock()
	defer shellMu.Unlock()
	launched := map[string]string{}
	ended := map[string]bool{}
	for _, f := range files {
		l := updateShellLedger(f, since)
		if l == nil {
			continue
		}
		for task, id := range l.launched {
			launched[task] = id
		}
		for id := range l.ended {
			ended[id] = true
		}
	}
	n := 0
	for task, id := range launched {
		if !ended[task] && !ended[id] {
			n++
		}
	}
	return n
}

// updateShellLedger reads what was appended to path since the last call; nil
// means path holds no shell of the current run. Callers hold shellMu.
func updateShellLedger(path string, since time.Time) *shellLedger {
	fi, err := os.Stat(path)
	if err != nil || (!since.IsZero() && fi.ModTime().Before(since)) {
		delete(shellLedgers, path)
		return nil
	}
	l := shellLedgers[path]
	if l == nil || !l.since.Equal(since) || fi.Size() < l.size {
		l = &shellLedger{since: since, launched: map[string]string{}, ended: map[string]bool{}}
		shellLedgers[path] = l
	}
	if fi.Size() == l.size {
		return l
	}

	f, err := os.Open(path)
	if err != nil {
		return l
	}
	defer f.Close()
	data := make([]byte, fi.Size()-l.size)
	n, err := f.ReadAt(data, l.size)
	if err != nil && err != io.EOF {
		return l
	}
	data = data[:n]
	// A trailing partial line is still being written; read it next time.
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return l
	}
	l.size += int64(end + 1)
	for _, line := range bytes.Split(data[:end], []byte{'\n'}) {
		l.addLine(line)
	}
	return l
}

func (l *shellLedger) addLine(line []byte) {
	if !bytes.Contains(line, []byte("backgroundTaskId")) && !bytes.Contains(line, []byte("task-notification")) &&
		!bytes.Contains(line, []byte(`"TaskStop"`)) && !bytes.Contains(line, []byte(`"KillShell"`)) {
		return
	}
	var rec activityRecord
	if json.Unmarshal(line, &rec) != nil {
		return
	}
	switch rec.Type {
	case "attachment":
		var text string
		if a := rec.Attachment; a != nil && a.Type == "queued_command" && json.Unmarshal(a.Prompt, &text) == nil {
			l.notified(text)
		}

	case "assistant":
		var msg activityMessage
		var blocks []activityBlock
		if json.Unmarshal(rec.Message, &msg) != nil || json.Unmarshal(msg.Content, &blocks) != nil {
			return
		}
		for _, b := range blocks {
			if b.Type == "tool_use" && (b.Name == "TaskStop" || b.Name == "KillShell") {
				var in struct {
					TaskID  string `json:"task_id"`
					ShellID string `json:"shell_id"`
				}
				_ = json.Unmarshal(b.Input, &in)
				l.ended[in.TaskID] = true
				l.ended[in.ShellID] = true
			}
		}

	case "user":
		var msg activityMessage
		if json.Unmarshal(rec.Message, &msg) != nil {
			return
		}
		text, results := userContent(msg.Content)
		ts := parseTimestamp(rec.Timestamp)
		// Shells launched by an earlier run died with its process (e.g. a resumed session).
		if _, task := asyncLaunch(rec.ToolUseResult); task != "" && len(results) > 0 &&
			(l.since.IsZero() || ts.IsZero() || !ts.Before(l.since)) {
			l.launched[task] = results[0]
		}
		l.notified(text)
	}
}

// notified records the shell a <task-notification> reports finished; some
// notifications open with a system preamble, so the tag may not lead.
func (l *shellLedger) notified(text string) {
	if !strings.Contains(text, "<task-notification>") {
		return
	}
	if m := notificationToolUseID.FindStringSubmatch(text); m != nil {
		l.ended[m[1]] = true
	}
	if m := notificationTaskID.FindStringSubmatch(text); m != nil {
		l.ended[m[1]] = true
	}
}
