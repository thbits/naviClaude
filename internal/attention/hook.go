package attention

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/thbits/naviClaude/internal/config"
)

// Payload is the subset of the Claude Code hook input naviClaude reads.
type Payload struct {
	HookEventName    string            `json:"hook_event_name"`
	SessionID        string            `json:"session_id"`
	NotificationType string            `json:"notification_type"`
	ToolName         string            `json:"tool_name"`
	BackgroundTasks  []json.RawMessage `json:"background_tasks"`
}

// Classify maps a hook event to what the engine should do with it.
func Classify(p Payload) Kind {
	switch p.HookEventName {
	case "PermissionRequest":
		return KindAttention
	case "Notification":
		switch p.NotificationType {
		case "permission_prompt", "elicitation_dialog", "elicitation_url_dialog", "agent_needs_input":
			return KindAttention
		}
	case "PreToolUse":
		// Under bypass/auto modes these dialogs fire no PermissionRequest.
		if p.ToolName == "AskUserQuestion" || p.ToolName == "ExitPlanMode" {
			return KindAttention
		}
	case "Stop":
		// Background work will wake the session again; it is not done yet.
		if len(p.BackgroundTasks) == 0 {
			return KindDone
		}
	case "StopFailure":
		return KindDone
	case "UserPromptSubmit":
		return KindBusy
	case "SessionEnd":
		return KindClear
	}
	return KindIgnore
}

// RunHook handles one Claude Code hook invocation: the payload is on stdin and
// the pane is $TMUX_PANE. Clears run inline; other events re-exec a detached
// watcher so Claude never waits on the confirm delay, the sound or the turn.
func RunHook(stdin io.Reader) error {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" || os.Getenv("TMUX") == "" {
		return nil // outside tmux (e.g. herdr) the host app notifies
	}
	cfg, _ := config.Load("")
	if !cfg.Notifications.Enabled {
		return nil
	}
	var p Payload
	if err := json.NewDecoder(stdin).Decode(&p); err != nil {
		return fmt.Errorf("hook: decode payload: %w", err)
	}
	kind := Classify(p)
	debugLogf("hook %s pane=%s session=%s type=%s tool=%s -> %q", p.HookEventName, pane, p.SessionID, p.NotificationType, p.ToolName, kind)
	switch kind {
	case KindClear:
		newEngine(cfg.Notifications).End(pane)
	case KindAttention, KindDone, KindBusy:
		return spawnWatcher(kind, pane, p.SessionID)
	}
	return nil
}

func spawnWatcher(kind Kind, pane, sessionID string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "hook", "--watch", string(kind), pane, sessionID)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("hook: start watcher: %w", err)
	}
	return cmd.Process.Release()
}

// RunWatcher is the detached half of RunHook.
func RunWatcher(kind, pane, sessionID string) {
	cfg, _ := config.Load("")
	if Kind(kind) == KindBusy {
		newEngine(cfg.Notifications).Busy(pane, sessionID)
		return
	}
	newEngine(cfg.Notifications).Raise(Kind(kind), pane, sessionID)
}
