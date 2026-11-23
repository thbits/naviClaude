package attention

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/thbits/naviClaude/internal/config"
)

// Payload is the subset of the Claude Code hook input naviClaude reads.
type Payload struct {
	HookEventName    string            `json:"hook_event_name"`
	SessionID        string            `json:"session_id"`
	NotificationType string            `json:"notification_type"`
	ToolName         string            `json:"tool_name"`
	Message          string            `json:"message"`
	Cwd              string            `json:"cwd"`
	TranscriptPath   string            `json:"transcript_path"`
	ToolInput        json.RawMessage   `json:"tool_input"`
	LastMessage      string            `json:"last_assistant_message"` // Stop / StopFailure
	Error            string            `json:"error"`                  // StopFailure
	ErrorDetails     string            `json:"error_details"`          // StopFailure
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

// RunHook handles one hook call (payload on stdin, pane in $TMUX_PANE). Clears run
// inline; other events re-exec a detached watcher so Claude never waits on them.
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
		newHookEngine(cfg.Notifications, p.SessionID).End(pane)
	case KindBusy:
		return spawnWatcher(kind, pane, p.SessionID)
	case KindAttention, KindDone:
		return spawnWatcher(kind, pane, p.SessionID, bannerTitle(p), bannerDetail(p), bannerSubtitle(p))
	}
	return nil
}

func spawnWatcher(kind Kind, pane, sessionID string, extra ...string) error {
	// Claude ran us by the path stored in settings.json, which survives upgrades.
	exe := os.Args[0]
	if !filepath.IsAbs(exe) {
		var err error
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	cmd := exec.Command(exe, append([]string{"hook", "--watch", string(kind), pane, sessionID}, extra...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("hook: start watcher: %w", err)
	}
	return cmd.Process.Release()
}

// RunWatcher is the detached half of RunHook; extra is the banner title, detail and subtitle.
func RunWatcher(kind, pane, sessionID string, extra ...string) {
	cfg, _ := config.Load("")
	if Kind(kind) == KindBusy {
		newHookEngine(cfg.Notifications, sessionID).Busy(pane, sessionID)
		return
	}
	e := newHookEngine(cfg.Notifications, sessionID)
	if len(extra) > 0 {
		e.event.Title = extra[0]
	}
	if len(extra) > 1 {
		e.event.Detail = extra[1]
	}
	if len(extra) > 2 {
		e.event.Subtitle = extra[2]
	}
	e.Raise(Kind(kind), pane, sessionID)
}
