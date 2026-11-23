package attention

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/notify"
)

func withBanners(te *testEngine) *[]notify.Banner {
	var got []notify.Banner
	te.banner = func(b notify.Banner) error { got = append(got, b); return nil }
	return &got
}

func TestRaiseAwayPostsBanner(t *testing.T) {
	polls := 0
	te := newTestEngine(func() claude.NativeState {
		polls++
		if polls < 3 {
			return claude.NativeState{Status: "waiting", WaitingFor: "permission prompt", UpdatedAt: 7}
		}
		return claude.NativeState{Status: "busy", UpdatedAt: 9}
	})
	te.fake.focused = false
	got := withBanners(te)
	te.event = eventInfo{Title: "naviClaude", Detail: "needs approval: Edit"}

	te.Raise(KindAttention, "%1", "sid")

	want := notify.Banner{Title: "naviClaude", Message: "needs approval: Edit", Pane: "%1"}
	if len(*got) != 1 || (*got)[0] != want {
		t.Errorf("banners = %+v, want [%+v]", *got, want)
	}
}

func TestRaiseDoneAwayFallsBackToFinished(t *testing.T) {
	polls := 0
	te := newTestEngine(func() claude.NativeState {
		polls++
		if polls < 3 {
			return claude.NativeState{Status: "idle", UpdatedAt: 7}
		}
		return claude.NativeState{Status: "busy", UpdatedAt: 9}
	})
	te.fake.focused = false
	got := withBanners(te)

	te.Raise(KindDone, "%1", "sid")

	want := notify.Banner{Title: "Claude Code", Message: "Finished", Pane: "%1"}
	if len(*got) != 1 || (*got)[0] != want {
		t.Errorf("banners = %+v, want [%+v]", *got, want)
	}
}

func TestRaiseVisibleSkipsBanner(t *testing.T) {
	te := newTestEngine(func() claude.NativeState {
		return claude.NativeState{Status: "waiting", WaitingFor: "permission prompt", UpdatedAt: 7}
	})
	te.fake.panes["%1"].onScreen = true
	got := withBanners(te)
	te.Raise(KindAttention, "%1", "sid")
	if len(*got) != 0 {
		t.Errorf("visible pane got banners %+v", *got)
	}
	if len(te.played) != 1 {
		t.Errorf("visible attention should still ring, played %v", te.played)
	}
}

func TestRaiseDuplicatePromptPostsOneBanner(t *testing.T) {
	te := newTestEngine(func() claude.NativeState {
		return claude.NativeState{Status: "waiting", WaitingFor: "permission prompt", UpdatedAt: 7}
	})
	te.fake.focused = false
	te.maxLife = 0 // skip the watch loop
	got := withBanners(te)
	te.Raise(KindAttention, "%1", "sid")
	te.Raise(KindAttention, "%1", "sid")
	if len(*got) != 1 {
		t.Errorf("same prompt posted %d banners, want 1", len(*got))
	}
}

func TestRaiseWithinCooldownStillPostsBanner(t *testing.T) {
	te := newTestEngine(func() claude.NativeState { return claude.NativeState{Status: "idle", UpdatedAt: 7} })
	te.fake.focused = false
	te.maxLife = 0
	te.fake.lastRing = te.clock.UnixMilli() // another session just rang
	got := withBanners(te)
	te.Raise(KindDone, "%1", "sid")
	if len(te.played) != 0 || len(*got) != 1 {
		t.Errorf("played %v banners %+v; want silence but one banner", te.played, *got)
	}
}

func TestBannerText(t *testing.T) {
	cases := []struct {
		p            Payload
		title, field string
	}{
		{Payload{HookEventName: "PermissionRequest", ToolName: "Edit", Cwd: "/src/proj"}, "proj", "Approve Edit"},
		{Payload{HookEventName: "PermissionRequest", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"go test ./..."}`)}, "", "Approve Bash: go test ./..."},
		{Payload{HookEventName: "PermissionRequest", ToolName: "Edit", ToolInput: json.RawMessage(`{"file_path":"/src/proj/main.go"}`)}, "", "Approve Edit: main.go"},
		{Payload{HookEventName: "PreToolUse", ToolName: "AskUserQuestion"}, "", "Has a question for you"},
		{Payload{HookEventName: "PreToolUse", ToolName: "AskUserQuestion", ToolInput: json.RawMessage(`{"questions":[{"question":"Which key for detail?"}]}`)}, "", "Question: Which key for detail?"},
		{Payload{HookEventName: "PreToolUse", ToolName: "ExitPlanMode"}, "", "Plan ready for review"},
		{Payload{HookEventName: "PreToolUse", ToolName: "ExitPlanMode", ToolInput: json.RawMessage(`{"plan":"# Rework banners\n\n1. Title"}`)}, "", "Plan ready: Rework banners 1. Title"},
		{Payload{HookEventName: "Notification", NotificationType: "elicitation_dialog"}, "", "Input requested"},
		{Payload{HookEventName: "Notification", Message: "Claude needs your permission to use Bash"}, "", "Claude needs your permission to use Bash"},
		{Payload{HookEventName: "StopFailure"}, "", "Turn failed"},
		{Payload{HookEventName: "StopFailure", Error: "rate_limit", ErrorDetails: "Usage limit reached"}, "", "Failed: Usage limit reached"},
		{Payload{HookEventName: "Stop", Cwd: "/"}, "", "Finished"},
		{Payload{HookEventName: "Stop", LastMessage: "## Summary\n\nAll **tests** pass:\n```\nok ./...\n```\n- built `naviclaude`"}, "", "Summary All tests pass: built naviclaude"},
	}
	for _, c := range cases {
		if got := bannerTitle(c.p); got != c.title {
			t.Errorf("bannerTitle(%+v) = %q, want %q", c.p, got, c.title)
		}
		if got := bannerDetail(c.p); got != c.field {
			t.Errorf("bannerDetail(%+v) = %q, want %q", c.p, got, c.field)
		}
	}
	if desktopBanner("off", "") != nil || desktopBanner("", "") != nil || desktopBanner("system", "") == nil {
		t.Error("desktopBanner mode mapping wrong")
	}
}

func TestCondenseCutsLongReplies(t *testing.T) {
	got := condense(strings.Repeat("word ", 100))
	if r := []rune(got); len(r) > bannerMessageMax || !strings.HasSuffix(got, "…") {
		t.Errorf("condense = %q (%d runes), want at most %d ending in …", got, len(r), bannerMessageMax)
	}
}
