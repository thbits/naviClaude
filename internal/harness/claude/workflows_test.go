package claude

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

func workflowResult(sec int, toolID, task, runID, name string) string {
	return jl(map[string]any{"type": "user", "timestamp": ats(sec),
		"toolUseResult": map[string]any{"status": "async_launched", "taskId": task, "taskType": "local_workflow",
			"workflowName": name, "runId": runID, "summary": "s", "transcriptDir": "/elsewhere/" + runID},
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": toolID, "content": "ok"}}}})
}

func workflowNotification(sec int, task, toolID string) string {
	return prompt(sec, "<task-notification>\n<task-id>"+task+"</task-id>\n<tool-use-id>"+toolID+"</tool-use-id>\n<status>completed</status>\n<summary>Dynamic workflow \"x\" completed</summary>\n</task-notification>")
}

func started(id, label, phase string) string {
	return jl(map[string]any{"type": "started", "key": "k" + id, "agentId": id, "label": label, "phase": phase})
}

func finished(typ, id string) string {
	return jl(map[string]any{"type": typ, "key": "k" + id, "agentId": id, "result": map[string]any{"report": "r"}})
}

// writeWorkflow writes runID's journal (and state file, when status is set) beside transcript path.
func writeWorkflow(t *testing.T, path, runID, status string, journal ...string) {
	t.Helper()
	dir := strings.TrimSuffix(path, ".jsonl")
	jdir := filepath.Join(dir, "subagents", "workflows", runID)
	if err := os.MkdirAll(jdir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(append([]string{`{"type":"launched"}`}, journal...), "\n") + "\n"
	if err := os.WriteFile(filepath.Join(jdir, "journal.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if status == "" {
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, "workflows"), 0o700); err != nil {
		t.Fatal(err)
	}
	state := jl(map[string]any{"runId": runID, "status": status})
	if err := os.WriteFile(filepath.Join(dir, "workflows", runID+".json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadActivityWorkflows(t *testing.T) {
	launch := []string{prompt(0, "go"), toolUse(1, "w1", "Workflow", map[string]any{"script": "x"}),
		workflowResult(2, "w1", "wtask1", "wf_one", "design"), assistantText(3, "launched"), turnEnd(4, 0)}
	journal := []string{started("a1", "design:compat", "Design"), started("a2", "design:reuse", "Design"),
		finished("result", "a1"), started("a3", "review:infra", "Review"), finished("failed", "a2")}
	one := session.WorkflowRun{Name: "design", Phase: "Review", Agents: []string{"review:infra"}}

	tests := []struct {
		name    string
		lines   []string
		status  string // state file status; "" writes none
		journal []string
		since   time.Time
		want    []session.WorkflowRun
	}{
		{name: "running run lists its live agents", lines: launch, journal: journal, want: []session.WorkflowRun{one}},
		{name: "between phases it still runs with no agents", lines: launch,
			journal: []string{started("a1", "read", "Understand"), finished("result", "a1")},
			want:    []session.WorkflowRun{{Name: "design", Phase: "Understand"}}},
		{name: "no journal yet", lines: launch, want: []session.WorkflowRun{{Name: "design"}}},
		{name: "notification ends it", lines: append(append([]string(nil), launch...), workflowNotification(9, "wtask1", "w1")), journal: journal},
		{name: "stopping it ends it", lines: append(append([]string(nil), launch...),
			toolUse(8, "s1", "TaskStop", map[string]any{"task_id": "wtask1"}), toolResult(9, "s1", false)), journal: journal},
		{name: "state file with a final status ends it", lines: launch, status: "failed", journal: journal},
		{name: "state file still running keeps it", lines: launch, status: "running", journal: journal, want: []session.WorkflowRun{one}},
		{name: "an earlier run's launch is dropped", lines: append(append([]string(nil), launch...), prompt(120, "resumed")),
			journal: journal, since: actBase.Add(time.Minute)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTranscript(t, tt.lines...)
			if tt.journal != nil || tt.status != "" {
				writeWorkflow(t, path, "wf_one", tt.status, tt.journal...)
			}
			got, err := loadActivity(path, tt.since)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Workflows, tt.want) {
				t.Errorf("Workflows = %+v, want %+v", got.Workflows, tt.want)
			}
			if busy := got.Background().Busy(); busy != (len(tt.want) > 0) {
				t.Errorf("Busy = %v with workflows %+v", busy, got.Workflows)
			}
		})
	}
}

// Journal lines appended after a read are picked up, and a run's agents end with it.
func TestLoadActivityWorkflowJournalGrows(t *testing.T) {
	path := writeTranscript(t, prompt(0, "go"), toolUse(1, "w1", "Workflow", nil),
		workflowResult(2, "w1", "wtask1", "wf_one", "design"), turnEnd(3, 0))
	writeWorkflow(t, path, "wf_one", "", started("a1", "read:a", "Understand"))
	if got, _ := LoadActivity(path); len(got.Workflows) != 1 || len(got.Workflows[0].Agents) != 1 {
		t.Fatalf("Workflows = %+v, want one run with one agent", got.Workflows)
	}

	journal := filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "workflows", "wf_one", "journal.jsonl")
	f, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(finished("result", "a1") + "\n" + started("a2", "critic", "Critic") + "\n")
	f.Close()
	got, _ := LoadActivity(path)
	if want := []session.WorkflowRun{{Name: "design", Phase: "Critic", Agents: []string{"critic"}}}; !reflect.DeepEqual(got.Workflows, want) {
		t.Errorf("after the journal grew: %+v, want %+v", got.Workflows, want)
	}
}

// Workflow agents are not in pendingBackgroundAgentCount, so a run finishing
// must not lower the floor of plain background agents.
func TestWorkflowNotificationKeepsAgentFloor(t *testing.T) {
	path := writeTranscript(t, prompt(0, "go"), toolUse(1, "w1", "Workflow", nil),
		workflowResult(2, "w1", "wtask1", "wf_one", "design"), assistantText(3, "ok"), turnEnd(4, 2),
		workflowNotification(10, "wtask1", "w1"))
	if got, _ := LoadActivity(path); got.Agents != 2 || len(got.Workflows) != 0 {
		t.Errorf("agents = %d, workflows = %+v; want 2 agents and no run", got.Agents, got.Workflows)
	}
}

func TestWorkflowLaunchRejectsOtherResults(t *testing.T) {
	for _, raw := range []string{
		`{"isAsync":true,"status":"async_launched"}`,
		`{"status":"async_launched","taskId":"t","taskType":"local_bash","runId":"wf_x"}`,
		`{"status":"async_launched","taskId":"t","taskType":"local_workflow","runId":"../../etc"}`,
		`"text"`,
	} {
		if _, _, ok := workflowLaunch([]byte(raw)); ok {
			t.Errorf("%s was taken for a workflow launch", raw)
		}
	}
}
