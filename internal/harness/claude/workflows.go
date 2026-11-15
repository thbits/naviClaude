package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

// A Workflow tool run returns at once and reports its end by notification; its
// agents are logged in a journal under the session's subagents directory.

type wfLaunch struct {
	toolUseID, runID, name string
	at                     time.Time
}

// workflowLaunch reports a Workflow tool result's task id and run.
func workflowLaunch(raw json.RawMessage) (task string, w wfLaunch, ok bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return "", wfLaunch{}, false
	}
	var r struct {
		Status       string `json:"status"`
		TaskType     string `json:"taskType"`
		TaskID       string `json:"taskId"`
		RunID        string `json:"runId"`
		WorkflowName string `json:"workflowName"`
	}
	if json.Unmarshal(raw, &r) != nil || r.TaskType != "local_workflow" || r.Status != "async_launched" ||
		r.TaskID == "" || r.RunID == "" || filepath.Base(r.RunID) != r.RunID {
		return "", wfLaunch{}, false
	}
	return r.TaskID, wfLaunch{runID: r.RunID, name: r.WorkflowName}, true
}

type wfAgent struct{ label, phase string }

type wfJournal struct {
	size    int64
	started map[string]wfAgent
	order   []string // agent ids in start order
	ended   map[string]bool
	phase   string // phase of the latest agent started
}

// wfJournals caches each run's journal by path; guarded by shellMu.
var wfJournals = map[string]*wfJournal{}

// workflowRun reads the run's journal and reports what it has running, or
// ended when its state file shows it finished. Callers hold shellMu.
func workflowRun(sessionDir string, w wfLaunch) (run session.WorkflowRun, ended bool) {
	journal := journalPath(sessionDir, w.runID)
	if workflowFinished(filepath.Join(sessionDir, "workflows", w.runID+".json")) {
		delete(wfJournals, journal)
		return session.WorkflowRun{}, true
	}
	j := updateJournal(journal)
	run.Name = w.name
	if run.Name == "" {
		run.Name = w.runID
	}
	if j == nil {
		return run, false
	}
	run.Phase = j.phase
	for _, id := range j.order {
		if !j.ended[id] {
			run.Agents = append(run.Agents, j.started[id].label)
		}
	}
	return run, false
}

func journalPath(sessionDir, runID string) string {
	return filepath.Join(sessionDir, "subagents", "workflows", runID, "journal.jsonl")
}

// workflowFinished reports whether a run's state file, written when it ends,
// holds a final status.
func workflowFinished(stateFile string) bool {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return false
	}
	var st struct {
		Status string `json:"status"`
	}
	return json.Unmarshal(data, &st) == nil && st.Status != "" && st.Status != "running"
}

// updateJournal reads what was appended to a run's journal since the last call.
func updateJournal(path string) *wfJournal {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	j := wfJournals[path]
	if j == nil || fi.Size() < j.size {
		j = &wfJournal{started: map[string]wfAgent{}, ended: map[string]bool{}}
		wfJournals[path] = j
	}
	if fi.Size() == j.size {
		return j
	}
	lines, next, ok := appendedLines(path, j.size, fi.Size())
	if !ok {
		return j
	}
	j.size = next
	for _, line := range lines {
		var rec struct {
			Type    string `json:"type"`
			AgentID string `json:"agentId"`
			Label   string `json:"label"`
			Phase   string `json:"phase"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.AgentID == "" {
			continue
		}
		if rec.Type != "started" {
			// result, failed and any other outcome end the agent.
			j.ended[rec.AgentID] = true
			continue
		}
		if _, seen := j.started[rec.AgentID]; !seen {
			j.order = append(j.order, rec.AgentID)
		}
		j.started[rec.AgentID] = wfAgent{label: rec.Label, phase: rec.Phase}
		if rec.Phase != "" {
			j.phase = rec.Phase
		}
	}
	return j
}

// appendedLines returns the complete lines of path between from and size, and
// the offset after the last one; a trailing partial line waits for next time.
func appendedLines(path string, from, size int64) (lines [][]byte, next int64, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, from, false
	}
	defer f.Close()
	data := make([]byte, size-from)
	n, err := f.ReadAt(data, from)
	if err != nil && err != io.EOF {
		return nil, from, false
	}
	data = data[:n]
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, from, false
	}
	return bytes.Split(data[:end], []byte{'\n'}), from + int64(end+1), true
}
