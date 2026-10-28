package app

import (
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/ui"
)

// coreOnly implements just the required harness surface.
type coreOnly struct{}

func (coreOnly) Info() harness.Info                                  { return harness.Info{Name: "core", Label: "Core"} }
func (coreOnly) MatchProcess(string) bool                            { return false }
func (coreOnly) Enrich(*session.Session)                             {}
func (coreOnly) ClassifyPane(string) session.PaneSignal              { return session.SignalNone }
func (coreOnly) RecentlyActive(*session.Session, time.Duration) bool { return false }
func (coreOnly) StartCommand() string                                { return "core" }
func (coreOnly) NativeStatus(*session.Session) (session.SessionStatus, bool) {
	return session.StatusIdle, false
}

func TestCoreOnlyHarnessDegradesQuietly(t *testing.T) {
	m := Model{registry: harness.NewRegistry(coreOnly{})}
	sess := &session.Session{ID: "x", Harness: "core", Status: session.StatusClosed}
	h := m.harnessFor(sess)

	if got := m.menuCaps(sess); got != (ui.MenuCaps{}) {
		t.Errorf("menuCaps = %+v, want no resume/fork", got)
	}
	if msg := loadMetricsCmd(h, sess)().(metricsMsg); msg.metrics != nil {
		t.Errorf("metrics = %+v, want nil", msg.metrics)
	}
	if msg := loadDetailDataCmd(h, sess)().(detailDataMsg); msg.messageCount != 0 {
		t.Errorf("detail messageCount = %d, want 0", msg.messageCount)
	}
	if msg := loadChangedFilesCmd(h, sess)().(changedFilesMsg); len(msg.files) != 0 {
		t.Errorf("changed files = %v, want none", msg.files)
	}
	if msg := m.loadClosedPreviewCmd(sess)().(closedPreviewMsg); msg.err != nil {
		t.Errorf("closed preview err = %v, want quiet empty content", msg.err)
	}
	if _, err := resumeCommandFor(h, sess, false); err == nil {
		t.Error("resume must fail for a harness without Resumer")
	}
	if _, ok := m.statsProvider(); ok {
		t.Error("core-only registry must have no stats provider")
	}
	if _, cmd := m.openResumePicker(sess, false); cmd != nil {
		t.Error("openResumePicker must be a quiet no-op without Resumer")
	}
}

func TestClaudeHarnessCaps(t *testing.T) {
	m := Model{registry: harness.NewRegistry(claude.New(claude.Options{}))}
	closed := &session.Session{ID: "abc", Harness: "claude", Status: session.StatusClosed}
	if got := m.menuCaps(closed); !got.CanResume || !got.CanFork {
		t.Errorf("claude caps = %+v, want resume+fork", got)
	}
	// Unstamped sessions resolve to the default harness.
	if got := m.menuCaps(&session.Session{ID: "abc"}); !got.CanResume {
		t.Errorf("unstamped session should resolve to claude caps, got %+v", got)
	}
	cmd, err := resumeCommandFor(m.harnessFor(closed), closed, true)
	if err != nil || cmd != `claude --resume "abc" --fork-session` {
		t.Errorf("fork command = %q, %v", cmd, err)
	}
	if m.defaultInfo().Label != "Claude" {
		t.Errorf("default label = %q", m.defaultInfo().Label)
	}
}

func TestPartitionByHarness(t *testing.T) {
	reg := harness.NewRegistry(claude.New(claude.Options{}), coreOnly{})
	list := []*session.Session{
		{ID: "a"}, {ID: "b", Harness: "claude"}, {ID: "c", Harness: "core"}, {ID: "d", Harness: "gone"},
	}
	got := partitionByHarness(reg, list)
	if len(got["claude"]) != 2 || len(got["core"]) != 1 || len(got["gone"]) != 0 {
		t.Errorf("partition = %v", got)
	}
}
