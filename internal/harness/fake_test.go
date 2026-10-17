package harness

import (
	"time"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/stats"
)

// coreHarness implements only the required Harness surface.
type coreHarness struct {
	name      string
	procs     map[string]bool
	native    *session.SessionStatus
	signal    session.PaneSignal
	recent    bool
	enrichID  string
	classifyN int
}

func (f *coreHarness) Info() Info                    { return Info{Name: f.name, Label: f.name} }
func (f *coreHarness) MatchProcess(name string) bool { return f.procs[name] }
func (f *coreHarness) Enrich(s *session.Session)     { s.ID = f.enrichID }
func (f *coreHarness) NativeStatus(*session.Session) (session.SessionStatus, bool) {
	if f.native == nil {
		return 0, false
	}
	return *f.native, true
}
func (f *coreHarness) ClassifyPane(string) session.PaneSignal {
	f.classifyN++
	return f.signal
}
func (f *coreHarness) RecentlyActive(*session.Session, time.Duration) bool { return f.recent }
func (f *coreHarness) StartCommand() string                                { return f.name }

// fullHarness adds every optional capability.
type fullHarness struct{ coreHarness }

func (f *fullHarness) ResumeCommand(*session.Session) string { return "resume" }
func (f *fullHarness) ForkCommand(*session.Session) string   { return "fork" }
func (f *fullHarness) ScanClosedAndAll(float64, map[string]bool) ([]*session.Session, []*session.Session, error) {
	return nil, nil, nil
}
func (f *fullHarness) Summaries() (map[string]string, error) { return nil, nil }
func (f *fullHarness) LoadConversation(*session.Session, int) ([]session.ConversationEntry, error) {
	return nil, nil
}
func (f *fullHarness) LoadMetrics(*session.Session) (*session.SessionMetrics, error) {
	return nil, nil
}
func (f *fullHarness) LoadChangedFiles(*session.Session) ([]session.ChangedFile, error) {
	return nil, nil
}
func (f *fullHarness) ChangeStamp(*session.Session) time.Time         { return time.Time{} }
func (f *fullHarness) ComputeStats(int, string) (*stats.Stats, error) { return nil, nil }
