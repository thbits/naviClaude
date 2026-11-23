package attention

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/notify"
	"github.com/thbits/naviClaude/internal/session"
)

// eventInfo is what the hook payload said about an alert, for its banner.
type eventInfo struct {
	Title    string // the session's name
	Subtitle string // where it runs: project and branch
	Detail   string // what happened, e.g. "Approve Bash: go test ./..." or Claude's reply
}

// bannerMessageMax keeps a banner's body to the two lines Notification Center shows.
const bannerMessageMax = 160

// desktopBanner returns the banner poster for mode, or nil when banners are off.
func desktopBanner(mode, click string) func(notify.Banner) error {
	if mode == "" || mode == notify.DesktopOff {
		return nil
	}
	// The watcher is already detached from Claude, so it can wait to see a failure and fall back.
	n := notify.NewNotifier(mode, true)
	n.Click = click
	return n.Notify
}

// bannerTitle names the session as the sidebar does: your rename, Claude's
// title, then the project directory.
func bannerTitle(p Payload) string {
	if p.SessionID != "" {
		if alias := session.NewAliasStore(session.DefaultAliasPath()).All()[p.SessionID]; alias != "" {
			return alias
		}
		if t := claude.SessionTitle(p.SessionID, p.TranscriptPath); t != "" {
			return t
		}
	}
	return projectName(p.Cwd)
}

// bannerSubtitle says where the session runs, e.g. "naviClaude ⎇ main".
func bannerSubtitle(p Payload) string {
	sub := projectName(p.Cwd)
	if ref, ok := session.CurrentBranch(p.Cwd); ok && ref.Branch != "" {
		sub = strings.TrimSpace(sub + " ⎇ " + ref.Branch)
	}
	return sub
}

func projectName(cwd string) string {
	if cwd == "" || cwd == "/" {
		return ""
	}
	return filepath.Base(cwd)
}

// bannerDetail says what happened or what the event wants from the user; ""
// lets the banner fall back to Claude's own waitingFor.
func bannerDetail(p Payload) string {
	switch p.HookEventName {
	case "PermissionRequest":
		return approvalDetail(p)
	case "Notification":
		if p.Message != "" {
			return condense(p.Message)
		}
		switch p.NotificationType {
		case "permission_prompt":
			return "Needs approval"
		case "elicitation_dialog", "elicitation_url_dialog":
			return "Input requested"
		}
	case "PreToolUse":
		switch p.ToolName {
		case "AskUserQuestion":
			if q := firstQuestion(p.ToolInput); q != "" {
				return condense("Question: " + q)
			}
			return "Has a question for you"
		case "ExitPlanMode":
			var in struct {
				Plan string `json:"plan"`
			}
			_ = json.Unmarshal(p.ToolInput, &in)
			if head := condense(in.Plan); head != "" {
				return condense("Plan ready: " + head)
			}
			return "Plan ready for review"
		}
	case "Stop":
		if msg := condense(p.LastMessage); msg != "" {
			return msg
		}
		return "Finished"
	case "StopFailure":
		why := p.ErrorDetails
		if why == "" {
			why = p.Error
		}
		if why = condense(why); why != "" {
			return condense("Failed: " + why)
		}
		return "Turn failed"
	}
	return ""
}

func approvalDetail(p Payload) string {
	if p.ToolName == "" {
		return "Needs approval"
	}
	name, target := claude.ToolSummary(p.ToolName, p.ToolInput)
	if target == "" {
		return "Approve " + name
	}
	return condense("Approve " + name + ": " + target)
}

func firstQuestion(input json.RawMessage) string {
	var in struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(input, &in)
	if len(in.Questions) == 0 {
		return ""
	}
	return in.Questions[0].Question
}

// condense flattens markdown to one line of plain text, cut at bannerMessageMax runes.
func condense(s string) string {
	var parts []string
	inFence := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" || strings.Trim(line, "-*_|: ") == "" {
			continue
		}
		line = strings.TrimLeft(line, "#>*-+ ")
		parts = append(parts, line)
	}
	out := strings.Join(strings.Fields(strings.NewReplacer("**", "", "__", "", "`", "").Replace(strings.Join(parts, " "))), " ")
	if r := []rune(out); len(r) > bannerMessageMax {
		out = strings.TrimSpace(string(r[:bannerMessageMax-1])) + "…"
	}
	return out
}

// notifyDesktop posts the banner for a raised alert. It never fails the raise:
// errors are only logged.
func (e *engine) notifyDesktop(kind Kind, pane string, st claude.NativeState) {
	if e.banner == nil {
		return
	}
	b := notify.Banner{Title: e.event.Title, Subtitle: e.event.Subtitle, Message: e.event.Detail, Pane: pane}
	if b.Title == "" {
		b.Title = "Claude Code"
	}
	if b.Message == "" {
		switch {
		case kind == KindDone:
			b.Message = "Finished"
		case st.WaitingFor != "":
			b.Message = "Needs you: " + st.WaitingFor
		default:
			b.Message = "Needs you"
		}
	}
	if err := e.banner(b); err != nil {
		e.logf("banner %s: %v", pane, err)
	}
}
