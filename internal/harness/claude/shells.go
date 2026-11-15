package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

// Background shells and monitors often outlive the transcript tail parseActivity
// reads, and a subagent's are logged in its own transcript, so they are tallied
// from every transcript of the session, each read once and then incrementally.
// The ultracode toggle is tracked here too, since it also scrolls out of the tail.

type shellLedger struct {
	size      int64 // bytes consumed, ending at a line boundary
	since     time.Time
	launched  map[string]bgTask   // task id -> what it is
	ended     map[string]bool     // task and tool_use ids that completed or were stopped
	workflows map[string]wfLaunch // task id -> Workflow tool run
	ultracode bool                // the latest ultracode toggle of the current run turned it on
}

// background is the session-wide state countBackground gathers.
type background struct {
	shells, monitors int
	ultracode        bool
	workflows        []session.WorkflowRun
}

func (b background) applyTo(act *session.SessionActivity) {
	act.Shells, act.Monitors, act.Ultracode, act.Workflows = b.shells, b.monitors, b.ultracode, b.workflows
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

// countBackground returns the background shells, monitors and workflows still
// running in the session whose main transcript is path, launched at or after
// since, and whether that run has ultracode on.
func countBackground(path string, since time.Time) (out background) {
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
	workflows := map[string]wfLaunch{}
	for i, f := range files {
		l := updateShellLedger(f, since)
		if l == nil {
			continue
		}
		if i == 0 {
			out.ultracode = l.ultracode
		}
		for task, bt := range l.launched {
			launched[task] = bt
		}
		for task, w := range l.workflows {
			workflows[task] = w
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
			out.shells++
		case bt.expires.IsZero() || now.Before(bt.expires):
			out.monitors++
		}
	}
	out.workflows = activeWorkflows(path, workflows, ended)
	return out
}

// activeWorkflows returns the runs not yet reported ended, oldest first.
// Callers hold shellMu.
func activeWorkflows(path string, launched map[string]wfLaunch, ended map[string]bool) []session.WorkflowRun {
	dir := strings.TrimSuffix(path, ".jsonl")
	var live []wfLaunch
	for task, w := range launched {
		if ended[task] || ended[w.toolUseID] {
			delete(wfJournals, journalPath(dir, w.runID))
		} else {
			live = append(live, w)
		}
	}
	slices.SortFunc(live, func(a, b wfLaunch) int {
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
		return strings.Compare(a.runID, b.runID)
	})
	var runs []session.WorkflowRun
	for _, w := range live {
		if run, done := workflowRun(dir, w); !done {
			runs = append(runs, run)
		}
	}
	return runs
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
		l = &shellLedger{since: since, launched: map[string]bgTask{}, ended: map[string]bool{}, workflows: map[string]wfLaunch{}}
		shellLedgers[path] = l
	}
	if fi.Size() == l.size {
		return l
	}

	lines, next, ok := appendedLines(path, l.size, fi.Size())
	if !ok {
		return l
	}
	l.size = next
	for _, line := range lines {
		l.addLine(line)
	}
	return l
}

func (l *shellLedger) addLine(line []byte) {
	if !bytes.Contains(line, []byte("backgroundTaskId")) && !bytes.Contains(line, []byte(`"timeoutMs"`)) &&
		!bytes.Contains(line, []byte("task-notification")) &&
		!bytes.Contains(line, []byte(`"TaskStop"`)) && !bytes.Contains(line, []byte(`"KillShell"`)) &&
		!bytes.Contains(line, []byte("ultra_effort_")) && !bytes.Contains(line, []byte("Ultracode o")) &&
		!bytes.Contains(line, []byte("local_workflow")) {
		return
	}
	var rec activityRecord
	if json.Unmarshal(line, &rec) != nil {
		return
	}
	ts := parseTimestamp(rec.Timestamp)
	// An earlier run's toggle died with its process; a still-on mode is re-announced.
	currentRun := l.since.IsZero() || ts.IsZero() || !ts.Before(l.since)
	switch rec.Type {
	case "attachment":
		var text string
		a := rec.Attachment
		switch {
		case a == nil:
		case a.Type == "queued_command" && json.Unmarshal(a.Prompt, &text) == nil:
			l.notified(text)
		case a.Type == "ultra_effort_enter" && currentRun:
			l.ultracode = true
		case a.Type == "ultra_effort_exit" && currentRun:
			l.ultracode = false
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
		if on, ok := ultracodeToggle(text); ok && currentRun {
			l.ultracode = on
		}
		// Tasks launched by an earlier run died with its process (e.g. a resumed session).
		if len(results) > 0 && currentRun {
			if _, task := asyncLaunch(rec.ToolUseResult); task != "" {
				l.launched[task] = bgTask{toolUseID: results[0]}
			} else if task, expires, ok := monitorLaunch(rec.ToolUseResult, ts); ok {
				l.launched[task] = bgTask{toolUseID: results[0], monitor: true, expires: expires}
			} else if task, w, ok := workflowLaunch(rec.ToolUseResult); ok {
				w.toolUseID, w.at = results[0], ts
				l.workflows[task] = w
			}
		}
		l.notified(text)
	}
}

// ultracodeToggle reads the /effort command's output, which lands a prompt
// before the ultra_effort attachment does.
func ultracodeToggle(text string) (on, ok bool) {
	out, found := strings.CutPrefix(strings.TrimSpace(text), "<local-command-stdout>")
	if !found {
		return false, false
	}
	out = strings.TrimSuffix(out, "</local-command-stdout>")
	switch {
	case strings.HasPrefix(out, "Ultracode on") || strings.HasSuffix(out, "· Ultracode on"):
		return true, true
	case strings.HasPrefix(out, "Ultracode off") || strings.HasSuffix(out, "· Ultracode off"):
		return false, true
	}
	return false, false
}

// notified records the tasks the <task-notification> blocks in text report
// finished; the tag may follow a system preamble. A monitor's events carry no
// status, only its final notice does.
func (l *shellLedger) notified(text string) {
	for _, b := range notificationBlocks(text) {
		if !strings.Contains(b, "<status>") {
			continue
		}
		if m := notificationToolUseID.FindStringSubmatch(b); m != nil {
			l.ended[m[1]] = true
		}
		if m := notificationTaskID.FindStringSubmatch(b); m != nil {
			l.ended[m[1]] = true
		}
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
