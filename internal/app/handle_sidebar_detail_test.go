package app

import (
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
)

// blockingActivity parks LoadActivity until release is closed, standing in
// for a transcript read on a hung mount.
type blockingActivity struct {
	coreOnly
	entered chan *session.Session
	release chan struct{}
}

func (b blockingActivity) LoadActivity(s *session.Session) (*session.SessionActivity, error) {
	b.entered <- s
	<-b.release
	return &session.SessionActivity{}, nil
}

func TestSidebarDetailCmdSkipsWhileInFlight(t *testing.T) {
	h := blockingActivity{entered: make(chan *session.Session, 4), release: make(chan struct{})}
	sess := &session.Session{ID: "detail-inflight", Status: session.StatusActive}

	first := make(chan any, 1)
	go func() { first <- loadSidebarDetailCmd(h, sess)() }()
	select {
	case <-h.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first load never reached LoadActivity")
	}

	if msg := loadSidebarDetailCmd(h, sess)(); msg != nil {
		t.Errorf("second load while one is in flight = %#v, want nil", msg)
	}

	close(h.release)
	if msg, ok := (<-first).(sidebarDetailMsg); !ok || msg.sessionID != sess.ID {
		t.Errorf("first load = %#v, want a sidebarDetailMsg for %q", msg, sess.ID)
	}
	if _, ok := loadSidebarDetailCmd(h, sess)().(sidebarDetailMsg); !ok {
		t.Error("a load after the first finished must run again")
	}
}

func TestSidebarDetailCmdReadsSnapshot(t *testing.T) {
	h := blockingActivity{entered: make(chan *session.Session, 1), release: make(chan struct{})}
	close(h.release)
	sess := &session.Session{ID: "detail-snap", Status: session.StatusActive, SessionFile: "before"}
	cmd := loadSidebarDetailCmd(h, sess)
	sess.ID, sess.SessionFile = "changed", "after"

	msg := cmd().(sidebarDetailMsg)
	got := <-h.entered
	if msg.sessionID != "detail-snap" || got.ID != "detail-snap" || got.SessionFile != "before" {
		t.Errorf("msg id %q, LoadActivity saw id %q file %q; want the values at Cmd creation", msg.sessionID, got.ID, got.SessionFile)
	}
	if got == sess {
		t.Error("LoadActivity must not receive the sidebar's own session pointer")
	}
}
