package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/tmux"
)

func restoreRegistry() *harness.Registry {
	return harness.NewRegistry(claude.New(claude.Options{}), coreOnly{})
}

func allExist(string) bool { return true }

func workEntry(id string) session.LiveEntry {
	return session.LiveEntry{ID: id, Harness: "claude", CWD: "/p", Project: "p", SessionFile: "/t/" + id + ".jsonl", TmuxSession: "work", WindowIndex: 2, PaneIndex: 1}
}

func shellPane(target, cmd, path string) tmux.PaneInfo {
	sess, win, pane, _ := session.ParseTarget(target)
	return tmux.PaneInfo{SessionName: sess, WindowIndex: win, PaneIndex: pane, Target: target, CurrentCommand: cmd, CurrentPath: path}
}

func TestLiveEntriesFilters(t *testing.T) {
	active := []*session.Session{
		{ID: "keep", Harness: "claude", CWD: "/p", ProjectName: "p", SessionFile: "/t/keep.jsonl", TmuxTarget: "work:2.1"},
		{ID: "", Harness: "claude", SessionFile: "/t/x.jsonl", TmuxTarget: "work:1.0"},
		{ID: "nofile", Harness: "claude", SessionFile: "/t/missing.jsonl", TmuxTarget: "work:1.1"},
		{ID: "notarget", Harness: "claude", SessionFile: "/t/notarget.jsonl"},
		{ID: "noresume", Harness: "core", SessionFile: "/t/noresume.jsonl", TmuxTarget: "work:3.0"},
	}
	exists := func(p string) bool { return p != "/t/missing.jsonl" }

	got := liveEntries(active, restoreRegistry(), exists)
	if len(got) != 1 {
		t.Fatalf("liveEntries kept %d, want 1: %+v", len(got), got)
	}
	e := got[0]
	if e.ID != "keep" || e.TmuxSession != "work" || e.WindowIndex != 2 || e.PaneIndex != 1 || e.CWD != "/p" || e.Harness != "claude" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestLiveEntriesDefaultsHarnessToClaude(t *testing.T) {
	active := []*session.Session{{ID: "a", SessionFile: "/t/a.jsonl", TmuxTarget: "w:0.0"}}
	got := liveEntries(active, restoreRegistry(), allExist)
	if len(got) != 1 || got[0].Harness != "claude" {
		t.Fatalf("liveEntries = %+v, want one claude entry", got)
	}
}

func planOne(t *testing.T, e session.LiveEntry, panes []tmux.PaneInfo) restoreAction {
	t.Helper()
	actions, skipped := planRestore([]session.LiveEntry{e}, panes, nil, restoreRegistry(), allExist)
	if len(actions) != 1 || skipped != 0 {
		t.Fatalf("planRestore = %+v skipped=%d, want one action", actions, skipped)
	}
	return actions[0]
}

func TestPlanSamePane(t *testing.T) {
	a := planOne(t, workEntry("id1"), []tmux.PaneInfo{shellPane("work:2.1", "zsh", "/p")})
	if a.Kind != restoreSamePane || a.Pane != "work:2.1" {
		t.Fatalf("action = %+v, want same pane work:2.1", a)
	}
	if a.Command != `claude --resume "id1"` {
		t.Fatalf("Command = %q", a.Command)
	}
}

func TestPlanLoginShellDashPrefix(t *testing.T) {
	a := planOne(t, workEntry("id1"), []tmux.PaneInfo{shellPane("work:2.1", "-zsh", "/p")})
	if a.Kind != restoreSamePane {
		t.Fatalf("Kind = %v, want same pane for a login shell", a.Kind)
	}
}

func TestPlanNonShellPaneUsesNewWindow(t *testing.T) {
	a := planOne(t, workEntry("id1"), []tmux.PaneInfo{shellPane("work:2.1", "nvim", "/p")})
	if a.Kind != restoreNewWindow {
		t.Fatalf("Kind = %v, want new window when the pane runs nvim", a.Kind)
	}
}

func TestPlanCwdMismatchUsesNewWindow(t *testing.T) {
	a := planOne(t, workEntry("id1"), []tmux.PaneInfo{shellPane("work:2.1", "zsh", "/other")})
	if a.Kind != restoreNewWindow {
		t.Fatalf("Kind = %v, want new window when the pane is in another dir", a.Kind)
	}
}

func TestPlanSessionMissingRecreates(t *testing.T) {
	a := planOne(t, workEntry("id1"), []tmux.PaneInfo{shellPane("other:0.0", "zsh", "/p")})
	if a.Kind != restoreRecreate {
		t.Fatalf("Kind = %v, want recreate when tmux session work is gone", a.Kind)
	}
}

func TestPlanSkipsLive(t *testing.T) {
	live := []*session.Session{{ID: "id1", Harness: "claude"}}
	actions, skipped := planRestore([]session.LiveEntry{workEntry("id1")}, nil, live, restoreRegistry(), allExist)
	if len(actions) != 0 || skipped != 1 {
		t.Fatalf("actions=%+v skipped=%d, want 0 actions and 1 skipped", actions, skipped)
	}
}

func TestPlanSkipsMissingTranscript(t *testing.T) {
	none := func(string) bool { return false }
	actions, skipped := planRestore([]session.LiveEntry{workEntry("id1")}, nil, nil, restoreRegistry(), none)
	if len(actions) != 0 || skipped != 1 {
		t.Fatalf("actions=%+v skipped=%d, want skip without a transcript", actions, skipped)
	}
}

func TestPlanSkipsNonResumer(t *testing.T) {
	for _, name := range []string{"core", "gone"} {
		e := workEntry("id1")
		e.Harness = name
		actions, skipped := planRestore([]session.LiveEntry{e}, nil, nil, restoreRegistry(), allExist)
		if len(actions) != 0 || skipped != 1 {
			t.Fatalf("harness %q: actions=%+v skipped=%d, want skipped", name, actions, skipped)
		}
	}
}

func TestPlanPaneClaimedOnce(t *testing.T) {
	panes := []tmux.PaneInfo{shellPane("work:2.1", "zsh", "/p")}
	actions, _ := planRestore([]session.LiveEntry{workEntry("a"), workEntry("b")}, panes, nil, restoreRegistry(), allExist)
	if len(actions) != 2 || actions[0].Kind != restoreSamePane || actions[1].Kind != restoreNewWindow {
		t.Fatalf("actions = %+v, want same pane then new window", actions)
	}
}

// fakeRestorer wires a restorer to in-memory fakes and records what it did.
type fakeRestorer struct {
	r         restorer
	slept     []time.Duration
	clock     time.Time
	listings  [][]tmux.PaneInfo
	listCalls int
	inPane    []string
	inSession []string
}

func newFakeRestorer(t *testing.T, stored *session.LiveSnapshot, current tmux.ServerIdentity, serverAge time.Duration) *fakeRestorer {
	t.Helper()
	store := session.NewLiveStateStore(filepath.Join(t.TempDir(), "live-sessions.json"))
	if stored != nil {
		if _, err := store.SaveIfChanged(current.SocketPath, *stored); err != nil {
			t.Fatal(err)
		}
	}
	f := &fakeRestorer{clock: time.Unix(current.StartTime, 0).Add(serverAge)}
	f.r = restorer{
		store:          store,
		reg:            restoreRegistry(),
		serverIdentity: func() (tmux.ServerIdentity, error) { return current, nil },
		listPanes: func() ([]tmux.PaneInfo, error) {
			i := min(f.listCalls, len(f.listings)-1)
			f.listCalls++
			if i < 0 {
				return nil, nil
			}
			return f.listings[i], nil
		},
		detect: func() ([]*session.Session, error) { return nil, nil },
		resumeInPane: func(s *session.Session, pane, cmd string) error {
			f.inPane = append(f.inPane, pane)
			return nil
		},
		resumeInSession: func(s *session.Session, name, cmd string) (string, error) {
			f.inSession = append(f.inSession, name)
			return name + ":9.0", nil
		},
		exists:      allExist,
		lock:        func() (func(), error) { return func() {}, nil },
		paneCommand: func(string) (string, error) { return "zsh", nil },
		now:         func() time.Time { return f.clock },
		sleep: func(d time.Duration) {
			f.slept = append(f.slept, d)
			f.clock = f.clock.Add(d)
		},
	}
	return f
}

func (f *fakeRestorer) totalSlept() time.Duration {
	var total time.Duration
	for _, d := range f.slept {
		total += d
	}
	return total
}

var testServer = tmux.ServerIdentity{StartTime: 200, SocketPath: "/sock"}

func TestRunSameServerDoesNothing(t *testing.T) {
	stored := &session.LiveSnapshot{ServerStart: 200, Sessions: []session.LiveEntry{workEntry("gone")}}
	f := newFakeRestorer(t, stored, testServer, time.Hour)

	msg := f.r.run()
	if msg.err != nil || msg.restored != 0 || len(f.inPane)+len(f.inSession) != 0 || len(f.slept) != 0 {
		t.Fatalf("same server: msg=%+v inPane=%v inSession=%v slept=%v, want nothing done", msg, f.inPane, f.inSession, f.slept)
	}
	if msg.info != testServer {
		t.Fatalf("info = %+v, want %+v", msg.info, testServer)
	}
}

func TestRunNoSnapshot(t *testing.T) {
	f := newFakeRestorer(t, nil, testServer, time.Hour)
	msg := f.r.run()
	if msg.err != nil || msg.restored != 0 || msg.skipped != 0 || f.listCalls != 0 {
		t.Fatalf("no snapshot: msg=%+v listCalls=%d", msg, f.listCalls)
	}
}

func TestRunServerIdentityError(t *testing.T) {
	f := newFakeRestorer(t, nil, testServer, time.Hour)
	f.r.serverIdentity = func() (tmux.ServerIdentity, error) { return tmux.ServerIdentity{}, errors.New("no server") }
	if msg := f.r.run(); msg.err == nil {
		t.Fatal("want err when the server identity is unknown")
	}
}

func TestRunDetectErrorKeepsSnapshot(t *testing.T) {
	stored := &session.LiveSnapshot{ServerStart: 100, Sessions: []session.LiveEntry{workEntry("a")}}
	f := newFakeRestorer(t, stored, testServer, time.Hour)
	f.r.detect = func() ([]*session.Session, error) { return nil, errors.New("ps failed") }
	msg := f.r.run()
	if msg.err == nil || len(f.inPane)+len(f.inSession) != 0 {
		t.Fatalf("detect error: msg=%+v, want err and nothing resumed", msg)
	}
}

func TestRunRestoresAfterRestart(t *testing.T) {
	missing := workEntry("b")
	missing.TmuxSession = "gone"
	stored := &session.LiveSnapshot{ServerStart: 100, Sessions: []session.LiveEntry{workEntry("a"), missing}}
	f := newFakeRestorer(t, stored, testServer, 20*time.Second)
	f.listings = [][]tmux.PaneInfo{{shellPane("work:2.1", "zsh", "/p")}}

	msg := f.r.run()
	if msg.err != nil || msg.restored != 2 || len(msg.errs) != 0 {
		t.Fatalf("msg = %+v, want 2 restored", msg)
	}
	if len(f.inPane) != 1 || f.inPane[0] != "work:2.1" {
		t.Fatalf("inPane = %v, want [work:2.1]", f.inPane)
	}
	if len(f.inSession) != 1 || f.inSession[0] != "gone" {
		t.Fatalf("inSession = %v, want [gone]", f.inSession)
	}
}

func TestRunCollectsErrors(t *testing.T) {
	stored := &session.LiveSnapshot{ServerStart: 100, Sessions: []session.LiveEntry{workEntry("a")}}
	f := newFakeRestorer(t, stored, testServer, time.Hour)
	f.r.resumeInSession = func(*session.Session, string, string) (string, error) { return "", errors.New("boom") }

	msg := f.r.run()
	if msg.restored != 0 || len(msg.errs) != 1 {
		t.Fatalf("msg = %+v, want 0 restored and 1 error", msg)
	}
}

func TestWaitForResurrectWaitsForAge(t *testing.T) {
	f := newFakeRestorer(t, nil, testServer, 5*time.Second)
	f.listings = [][]tmux.PaneInfo{{shellPane("a:0.0", "zsh", "/")}}
	waitForResurrect(f.r, testServer)
	if len(f.slept) == 0 || f.slept[0] != 10*time.Second {
		t.Fatalf("slept = %v, want a first 10s wait for the 15s server age", f.slept)
	}
}

func TestWaitForResurrectWaitsForStablePanes(t *testing.T) {
	a, b := shellPane("a:0.0", "zsh", "/"), shellPane("b:0.0", "zsh", "/")
	f := newFakeRestorer(t, nil, testServer, time.Hour)
	f.listings = [][]tmux.PaneInfo{{a}, {a, b}, {a, b}}

	got := waitForResurrect(f.r, testServer)
	if len(got) != 2 {
		t.Fatalf("got %d panes, want the settled listing of 2", len(got))
	}
	if len(f.slept) != 2 || f.slept[0] != 2*time.Second || f.slept[1] != 2*time.Second {
		t.Fatalf("slept = %v, want two 2s waits", f.slept)
	}
}

func TestWaitForResurrectCapsAt60s(t *testing.T) {
	f := newFakeRestorer(t, nil, testServer, 0)
	for i := 0; i < 100; i++ {
		f.listings = append(f.listings, []tmux.PaneInfo{shellPane("s:"+strconv.Itoa(i)+".0", "zsh", "/")})
	}
	waitForResurrect(f.r, testServer)
	if f.totalSlept() > 60*time.Second {
		t.Fatalf("waited %v, want at most 60s", f.totalSlept())
	}
	if f.totalSlept() < 50*time.Second {
		t.Fatalf("waited only %v; an unsettled server should keep waiting up to the cap", f.totalSlept())
	}
}

// newRestoreModel is a minimal Model with a temp-backed live-state store and one
// live session whose transcript exists.
func newRestoreModel(t *testing.T) (Model, string) {
	t.Helper()
	m := newTestModel(t, config.DefaultConfig(), session.ViewState{})
	dir := t.TempDir()
	path := filepath.Join(dir, "live-sessions.json")
	m.liveState = session.NewLiveStateStore(path)
	m.registry = restoreRegistry()
	m.liveRecorder = newLiveRecorder()
	transcript := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.activeSessions = []*session.Session{{ID: "a", Harness: "claude", CWD: dir, SessionFile: transcript, TmuxTarget: "work:1.0"}}
	return m, path
}

func TestNoSnapshotWriteBeforeRestoreDone(t *testing.T) {
	m, path := newRestoreModel(t)
	if cmd := m.saveLiveStateCmd(m.activeSessions); cmd != nil {
		cmd()
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("snapshot written before the restore check finished (stat err=%v)", err)
	}
}

func TestSnapshotWrittenAfterRestoreDone(t *testing.T) {
	m, path := newRestoreModel(t)
	next, cmd := m.Update(restoreDoneMsg{info: tmux.ServerIdentity{StartTime: 200, SocketPath: "/s"}})
	m = next.(Model)
	if !m.liveStateChecked {
		t.Fatal("liveStateChecked = false after restoreDoneMsg")
	}
	if cmd != nil {
		cmd()
	}
	if save := m.saveLiveStateCmd(m.activeSessions); save != nil {
		save()
	}

	snap, ok := session.NewLiveStateStore(path).Get("/s")
	if !ok || snap.ServerStart != 200 || len(snap.Sessions) != 1 || snap.Sessions[0].ID != "a" {
		t.Fatalf("stored snapshot = %+v (ok=%v), want server 200 with session a", snap, ok)
	}
}

func TestRestoreDoneErrorKeepsGateClosed(t *testing.T) {
	m, _ := newRestoreModel(t)
	next, _ := m.Update(restoreDoneMsg{err: errors.New("no server")})
	if next.(Model).liveStateChecked {
		t.Fatal("gate opened although the restore check failed")
	}
}

func TestRestoreDoneNotice(t *testing.T) {
	m, _ := newRestoreModel(t)
	m.statusbar.SetSize(200)
	next, _ := m.Update(restoreDoneMsg{info: testServer, restored: 2})
	m = next.(Model)
	if view := m.statusbar.View(); !strings.Contains(view, "restored 2 sessions") {
		t.Fatalf("status bar = %q, want it to mention restored 2 sessions", view)
	}
}

func TestRestoreDoneReportsFailures(t *testing.T) {
	m, _ := newRestoreModel(t)
	next, _ := m.Update(restoreDoneMsg{info: testServer, restored: 1, errs: []error{errors.New("boom")}})
	m = next.(Model)
	if m.err == nil || !strings.Contains(m.err.Error(), "1 failed") {
		t.Fatalf("err = %v, want a failure report", m.err)
	}
}

func TestRestoreDoneBeforeFirstScanKeepsSnapshot(t *testing.T) {
	m, path := newRestoreModel(t)
	m.activeSessions = nil
	stored := session.LiveSnapshot{ServerStart: 200, Sessions: []session.LiveEntry{workEntry("kept")}}
	if _, err := m.liveState.SaveIfChanged("/s", stored); err != nil {
		t.Fatal(err)
	}

	_, cmd := m.Update(restoreDoneMsg{info: tmux.ServerIdentity{StartTime: 200, SocketPath: "/s"}})
	if cmd != nil {
		cmd()
	}
	snap, _ := session.NewLiveStateStore(path).Get("/s")
	if len(snap.Sessions) != 1 || snap.Sessions[0].ID != "kept" {
		t.Fatalf("snapshot = %+v; restore finishing before the first scan wiped it", snap)
	}
}

func TestRecorderKeepsBrieflyMissingEntries(t *testing.T) {
	r := newLiveRecorder()
	t0 := time.Unix(1000, 0)
	a, b := workEntry("a"), workEntry("b")
	b.WindowIndex = 3

	if got := r.record(t0, []session.LiveEntry{a, b}); len(got) != 2 {
		t.Fatalf("first record = %+v, want a and b", got)
	}
	if got := r.record(t0.Add(time.Second), []session.LiveEntry{a}); len(got) != 2 {
		t.Fatalf("b vanished for 1s and was dropped: %+v", got)
	}
	if got := r.record(t0.Add(liveEntryGrace+time.Second), []session.LiveEntry{a}); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("after the grace period = %+v, want only a", got)
	}
}

func TestRecorderOrdersByLocation(t *testing.T) {
	r := newLiveRecorder()
	late, early := workEntry("late"), workEntry("early")
	late.WindowIndex, early.WindowIndex = 9, 1
	got := r.record(time.Unix(1, 0), []session.LiveEntry{late, early})
	if len(got) != 2 || got[0].ID != "early" || got[1].ID != "late" {
		t.Fatalf("record = %+v, want sorted by window", got)
	}
}

func TestRestoreDoneErrorIsShownAndRetried(t *testing.T) {
	m, _ := newRestoreModel(t)
	next, cmd := m.Update(restoreDoneMsg{err: errors.New("no server")})
	m = next.(Model)
	if m.err == nil || !strings.Contains(m.err.Error(), "no server") {
		t.Fatalf("err = %v, want the restore failure shown", m.err)
	}
	if cmd == nil {
		t.Fatal("no retry scheduled after a failed restore check")
	}
	if _, cmd := m.Update(restoreRetryMsg{}); cmd == nil {
		t.Fatal("restoreRetryMsg did not start another restore check")
	}
}

func TestRunBusyWhenAnotherInstanceRestores(t *testing.T) {
	stored := &session.LiveSnapshot{ServerStart: 100, Sessions: []session.LiveEntry{workEntry("a")}}
	f := newFakeRestorer(t, stored, testServer, time.Hour)
	f.r.lock = func() (func(), error) { return nil, errors.New("locked") }

	msg := f.r.run()
	if msg.err == nil || len(f.inPane)+len(f.inSession) != 0 {
		t.Fatalf("msg=%+v inPane=%v inSession=%v, want busy error and nothing resumed", msg, f.inPane, f.inSession)
	}
}

func TestRunMarksSnapshotConsumed(t *testing.T) {
	stored := &session.LiveSnapshot{ServerStart: 100, Sessions: []session.LiveEntry{workEntry("a")}}
	f := newFakeRestorer(t, stored, testServer, time.Hour)
	f.listings = [][]tmux.PaneInfo{{shellPane("work:2.1", "zsh", "/p")}}

	if msg := f.r.run(); msg.restored != 1 {
		t.Fatalf("first run = %+v, want 1 restored", msg)
	}
	if msg := f.r.run(); msg.restored != 0 || len(f.inPane) != 1 {
		t.Fatalf("second run = %+v inPane=%v; a consumed snapshot was restored again", msg, f.inPane)
	}
}

func TestRunRechecksPaneBeforeTyping(t *testing.T) {
	stored := &session.LiveSnapshot{ServerStart: 100, Sessions: []session.LiveEntry{workEntry("a")}}
	f := newFakeRestorer(t, stored, testServer, time.Hour)
	f.listings = [][]tmux.PaneInfo{{shellPane("work:2.1", "zsh", "/p")}}
	f.r.paneCommand = func(string) (string, error) { return "2.1.310", nil }

	msg := f.r.run()
	if len(f.inPane) != 0 || len(f.inSession) != 1 || msg.restored != 1 {
		t.Fatalf("inPane=%v inSession=%v msg=%+v; typed into a pane that started running claude", f.inPane, f.inSession, msg)
	}
}

func TestRunReportsResumedSessions(t *testing.T) {
	missing := workEntry("b")
	missing.TmuxSession = "gone"
	stored := &session.LiveSnapshot{ServerStart: 100, Sessions: []session.LiveEntry{workEntry("a"), missing}}
	f := newFakeRestorer(t, stored, testServer, 20*time.Second)
	f.listings = [][]tmux.PaneInfo{{shellPane("work:2.1", "zsh", "/p")}}

	msg := f.r.run()
	want := []resumedSession{{key: "a", target: "work:2.1"}, {key: "b", target: "gone:9.0"}}
	if !slices.Equal(msg.resumed, want) {
		t.Fatalf("resumed = %+v, want %+v", msg.resumed, want)
	}
}

func TestCheckCountsPendingRestore(t *testing.T) {
	stored := &session.LiveSnapshot{ServerStart: 100, Sessions: []session.LiveEntry{workEntry("a"), workEntry("b")}}
	f := newFakeRestorer(t, stored, testServer, time.Second)
	if _, n, err := f.r.check(); err != nil || n != 2 {
		t.Fatalf("check = %d, %v; want 2 pending", n, err)
	}
	if len(f.slept) != 0 || f.listCalls != 0 {
		t.Fatal("check must not wait for resurrect")
	}

	same := newFakeRestorer(t, &session.LiveSnapshot{ServerStart: 200, Sessions: stored.Sessions}, testServer, time.Hour)
	if _, n, _ := same.r.check(); n != 0 {
		t.Fatalf("same server: check = %d, want 0", n)
	}
}

func TestRestorePendingShowsNoticeAndRuns(t *testing.T) {
	m, _ := newRestoreModel(t)
	m.statusbar.SetSize(200)
	next, cmd := m.Update(restorePendingMsg{count: 3})
	m = next.(Model)
	if view := m.statusbar.View(); !strings.Contains(view, "restoring 3 sessions") {
		t.Fatalf("status bar = %q, want it to mention restoring 3 sessions", view)
	}
	if cmd == nil {
		t.Fatal("want the restore to run after the pending notice")
	}
}

func restoringModel(t *testing.T, since time.Time) Model {
	t.Helper()
	m, _ := newRestoreModel(t)
	m.Update(restoreDoneMsg{info: testServer, restored: 1})
	m.restoring = []resumedSession{{key: "a", target: "work:1.0", since: since}}
	return m
}

func TestMarkRestoringHidesHeuristicWaiting(t *testing.T) {
	now := time.Unix(1000, 0)
	m := restoringModel(t, now)
	s := &session.Session{ID: "a", TmuxTarget: "work:1.0", Status: session.StatusWaiting}
	m.markRestoring([]*session.Session{s}, now.Add(5*time.Second))
	if !s.Restoring || s.Status != session.StatusIdle {
		t.Fatalf("session = restoring %v status %v, want restoring and idle", s.Restoring, s.Status)
	}
}

func TestMarkRestoringMatchesPaneWhenIDDiffers(t *testing.T) {
	now := time.Unix(1000, 0)
	m := restoringModel(t, now)
	s := &session.Session{ID: "other", TmuxTarget: "work:1.0", Status: session.StatusWaiting}
	m.markRestoring([]*session.Session{s}, now)
	if !s.Restoring {
		t.Fatal("want the session in the restored pane marked restoring")
	}
}

func TestMarkRestoringEndsOnNativeStatus(t *testing.T) {
	now := time.Unix(1000, 0)
	m := restoringModel(t, now)
	s := &session.Session{ID: "a", TmuxTarget: "work:1.0", Status: session.StatusIdle, NativeStatus: true}
	m.markRestoring([]*session.Session{s}, now)
	if s.Restoring || len(m.restoring) != 0 {
		t.Fatalf("native idle status: restoring %v, %d left; want restoring over", s.Restoring, len(m.restoring))
	}

	// Claude's own waiting status is a real prompt and must show.
	m = restoringModel(t, now)
	s = &session.Session{ID: "a", TmuxTarget: "work:1.0", Status: session.StatusWaiting, NativeStatus: true, WaitingFor: "permission prompt"}
	m.markRestoring([]*session.Session{s}, now)
	if s.Restoring || s.Status != session.StatusWaiting || s.WaitingFor == "" {
		t.Fatalf("native waiting: restoring %v status %v, want the prompt shown", s.Restoring, s.Status)
	}
}

func TestMarkRestoringExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	m := restoringModel(t, now)
	s := &session.Session{ID: "a", TmuxTarget: "work:1.0", Status: session.StatusWaiting}
	m.markRestoring([]*session.Session{s}, now.Add(restoringMaxAge+time.Second))
	if s.Restoring || s.Status != session.StatusWaiting || len(m.restoring) != 0 {
		t.Fatalf("expired: restoring %v status %v, want the real status back", s.Restoring, s.Status)
	}
}

func TestRestoreDoneStartsRestoring(t *testing.T) {
	m, _ := newRestoreModel(t)
	next, _ := m.Update(restoreDoneMsg{info: testServer, restored: 1, resumed: []resumedSession{{key: "a", target: "work:1.0"}}})
	m = next.(Model)
	if len(m.restoring) != 1 || m.restoring[0].since.IsZero() {
		t.Fatalf("restoring = %+v, want one timed entry", m.restoring)
	}
}

func TestCaptureWaitingIgnoredWhileRestoring(t *testing.T) {
	m, _ := newRestoreModel(t)
	s := &session.Session{ID: "a", TmuxTarget: "work:1.0", Status: session.StatusIdle, Restoring: true}
	m.sessions = []*session.Session{s}
	m.updateSessionStatus("work:1.0", session.StatusWaiting)
	if s.Status != session.StatusIdle {
		t.Fatalf("status = %v, want idle while restoring", s.Status)
	}
}
