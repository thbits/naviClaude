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

// Background shells and monitors often outlive the transcript tail parseActivity
// reads, and a subagent's are logged in its own transcript, so they are tallied
// from every transcript of the session, each read once and then incrementally.

type shellLedger struct {
	size     int64 // bytes consumed, ending at a line boundary
	since    time.Time
	launched map[string]bgTask // task id -> what it is
	ended    map[string]bool   // task and tool_use ids that completed or were stopped
}

// bgTask is a background shell or Monitor launched by a tool call.
type bgTask struct {
	toolUseID string
	monitor   bool
	expires   time.Time // when a non-persistent monitor times out; zero otherwise
}

var (
	shellMu      sync.Mutex
	shellLedgers = map[string]*shellLedger{}
)

var notificationTaskID = regexp.MustCompile(`<task-id>([^<]+)</task-id>`)

// countBackground returns the background shells and monitors still running in
// the session whose main transcript is path, launched at or after since.
func countBackground(path string, since time.Time) (shells, monitors int) {
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
	launched := map[string]bgTask{}
	ended := map[string]bool{}
	for _, f := range files {
		l := updateShellLedger(f, since)
		if l == nil {
			continue
		}
		for task, bt := range l.launched {
			launched[task] = bt
		}
		for id := range l.ended {
			ended[id] = true
		}
	}
	now := time.Now()
	for task, bt := range launched {
		switch {
		case ended[task] || ended[bt.toolUseID]:
		case !bt.monitor:
			shells++
		case bt.expires.IsZero() || now.Before(bt.expires):
			monitors++
		}
	}
	return shells, monitors
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
		l = &shellLedger{since: since, launched: map[string]bgTask{}, ended: map[string]bool{}}
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
	if !bytes.Contains(line, []byte("backgroundTaskId")) && !bytes.Contains(line, []byte(`"timeoutMs"`)) &&
		!bytes.Contains(line, []byte("task-notification")) &&
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
		// Tasks launched by an earlier run died with its process (e.g. a resumed session).
		if len(results) > 0 && (l.since.IsZero() || ts.IsZero() || !ts.Before(l.since)) {
			if _, task := asyncLaunch(rec.ToolUseResult); task != "" {
				l.launched[task] = bgTask{toolUseID: results[0]}
			} else if task, expires, ok := monitorLaunch(rec.ToolUseResult, ts); ok {
				l.launched[task] = bgTask{toolUseID: results[0], monitor: true, expires: expires}
			}
		}
		l.notified(text)
	}
}

// notified records the task a <task-notification> reports finished; some
// notifications open with a system preamble, so the tag may not lead. A monitor's
// events carry no status, only its final notice does.
func (l *shellLedger) notified(text string) {
	if !strings.Contains(text, "<task-notification>") || !strings.Contains(text, "<status>") {
		return
	}
	if m := notificationToolUseID.FindStringSubmatch(text); m != nil {
		l.ended[m[1]] = true
	}
	if m := notificationTaskID.FindStringSubmatch(text); m != nil {
		l.ended[m[1]] = true
	}
}

// monitorLaunch reports a Monitor tool result's task id and, unless it is
// persistent, when it times out.
func monitorLaunch(raw json.RawMessage, launched time.Time) (task string, expires time.Time, ok bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return "", time.Time{}, false
	}
	var r struct {
		TaskID     string `json:"taskId"`
		TimeoutMs  int64  `json:"timeoutMs"`
		Persistent *bool  `json:"persistent"`
	}
	if json.Unmarshal(raw, &r) != nil || r.TaskID == "" || r.Persistent == nil {
		return "", time.Time{}, false
	}
	if !*r.Persistent && r.TimeoutMs > 0 && !launched.IsZero() {
		expires = launched.Add(time.Duration(r.TimeoutMs) * time.Millisecond)
	}
	return r.TaskID, expires, true
}
