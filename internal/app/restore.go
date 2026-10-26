package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
	"github.com/thbits/naviClaude/internal/tmux"
)

// restoreKind is where a restored session is resumed.
type restoreKind int

const (
	restoreSamePane  restoreKind = iota // the original pane, back as a shell
	restoreNewWindow                    // a new window in the original tmux session
	restoreRecreate                     // the original tmux session, created again
)

// restoreAction resumes one recorded session; Pane is set for restoreSamePane.
type restoreAction struct {
	Entry   session.LiveEntry
	Kind    restoreKind
	Pane    string
	Command string
}

// shellNames are the pane commands a resume may be typed into.
var shellNames = map[string]bool{
	"zsh": true, "bash": true, "fish": true, "sh": true,
	"dash": true, "ksh": true, "tcsh": true, "nu": true,
}

// isShell reports whether a pane command is an idle interactive shell.
func isShell(cmd string) bool {
	return shellNames[strings.TrimPrefix(cmd, "-")]
}

// liveEntries picks the live sessions worth restoring after a tmux restart.
func liveEntries(active []*session.Session, reg *harness.Registry, exists func(path string) bool) []session.LiveEntry {
	var out []session.LiveEntry
	for _, s := range active {
		if s.ID == "" || s.SessionFile == "" || !exists(s.SessionFile) {
			continue
		}
		tmuxSession, win, pane, ok := session.ParseTarget(s.TmuxTarget)
		if !ok {
			continue
		}
		name := s.Harness
		if name == "" {
			name = "claude"
		}
		h, ok := reg.Get(name)
		if !ok {
			continue
		}
		if _, ok := harness.AsResumer(h); !ok {
			continue
		}
		out = append(out, session.LiveEntry{
			ID:          s.ID,
			Harness:     name,
			CWD:         s.CWD,
			Project:     s.ProjectName,
			SessionFile: s.SessionFile,
			TmuxSession: tmuxSession,
			WindowIndex: win,
			PaneIndex:   pane,
		})
	}
	return out
}

// entrySession rebuilds the session fields a resume needs from a recorded entry.
func entrySession(e session.LiveEntry) *session.Session {
	return &session.Session{ID: e.ID, CWD: e.CWD, Harness: e.Harness, ProjectName: e.Project, SessionFile: e.SessionFile}
}

// planRestore decides where each recorded session is resumed, skipping ones that
// are running or can no longer be resumed.
func planRestore(entries []session.LiveEntry, panes []tmux.PaneInfo, live []*session.Session, reg *harness.Registry, exists func(path string) bool) (actions []restoreAction, skipped int) {
	liveKeys := make(map[string]bool, len(live))
	for _, s := range live {
		liveKeys[s.Key()] = true
	}
	byTarget := make(map[string]tmux.PaneInfo, len(panes))
	tmuxSessions := make(map[string]bool)
	for _, p := range panes {
		byTarget[p.Target] = p
		tmuxSessions[p.SessionName] = true
	}
	claimed := make(map[string]bool)

	for _, e := range entries {
		sess := entrySession(e)
		h, ok := reg.Get(e.Harness)
		if !ok || liveKeys[sess.Key()] || !exists(e.SessionFile) {
			skipped++
			continue
		}
		cmd, err := resumeCommandFor(h, sess, false)
		if err != nil {
			skipped++
			continue
		}

		a := restoreAction{Entry: e, Command: cmd, Kind: restoreRecreate}
		target := tmuxTarget(e)
		if p, ok := byTarget[target]; ok && !claimed[target] && isShell(p.CurrentCommand) &&
			filepath.Clean(p.CurrentPath) == filepath.Clean(e.CWD) {
			a.Kind, a.Pane = restoreSamePane, target
			claimed[target] = true
		} else if tmuxSessions[e.TmuxSession] {
			a.Kind = restoreNewWindow
		}
		actions = append(actions, a)
	}
	return actions, skipped
}

// tmuxTarget is the entry's "session:window.pane" address.
func tmuxTarget(e session.LiveEntry) string {
	return e.TmuxSession + ":" + strconv.Itoa(e.WindowIndex) + "." + strconv.Itoa(e.PaneIndex)
}

// Resurrect-settle timing: restore waits for the server to be this old, then for
// two pane listings this far apart to match, but never longer than the cap.
const (
	restoreMinServerAge = 15 * time.Second
	restorePollInterval = 2 * time.Second
	restoreMaxWait      = 60 * time.Second
	restoreRetryDelay   = 30 * time.Second
)

// restoreRetryMsg re-runs a startup restore check that could not complete.
type restoreRetryMsg struct{}

// restoreDoneMsg reports a startup restore; err means the check could not run and
// the stored snapshot must be kept for the next launch.
type restoreDoneMsg struct {
	info     tmux.ServerIdentity
	restored int
	resumed  []resumedSession
	skipped  int
	errs     []error
	err      error
}

// restorePendingMsg reports a restore about to wait for resurrect, so the user
// knows to keep naviClaude open.
type restorePendingMsg struct{ count int }

// resumedSession is a session a restore resumed, shown as restoring until it
// reports its own status.
type resumedSession struct {
	key, target string
	since       time.Time
}

// restorer resumes the sessions a previous tmux server was running; every
// dependency is a field so tests never touch tmux.
type restorer struct {
	store           *session.LiveStateStore
	reg             *harness.Registry
	serverIdentity  func() (tmux.ServerIdentity, error)
	listPanes       func() ([]tmux.PaneInfo, error)
	detect          func() ([]*session.Session, error)
	resumeInPane    func(s *session.Session, pane, command string) error
	resumeInSession func(s *session.Session, tmuxSession, command string) (string, error)
	exists          func(path string) bool
	lock            func() (release func(), err error)
	paneCommand     func(target string) (string, error)
	now             func() time.Time
	sleep           func(time.Duration)
}

// check returns how many stored sessions an earlier tmux server left to restore.
func (r restorer) check() (tmux.ServerIdentity, int, error) {
	info, err := r.serverIdentity()
	if err != nil {
		return info, 0, err
	}
	snap, ok := r.store.Get(info.SocketPath)
	if !ok || snap.ServerStart == info.StartTime {
		return info, 0, nil
	}
	return info, len(snap.Sessions), nil
}

// run restores the stored snapshot when it came from an earlier tmux server.
func (r restorer) run() restoreDoneMsg {
	info, n, err := r.check()
	if err != nil {
		return restoreDoneMsg{err: err}
	}
	if n == 0 {
		return restoreDoneMsg{info: info}
	}

	release, err := r.lock()
	if err != nil {
		return restoreDoneMsg{err: fmt.Errorf("another naviClaude is restoring: %w", err)}
	}
	defer release()
	// Another instance may have restored while this one waited for the lock.
	snap, ok := r.store.Get(info.SocketPath)
	if !ok || len(snap.Sessions) == 0 || snap.ServerStart == info.StartTime {
		return restoreDoneMsg{info: info}
	}

	panes := waitForResurrect(r, info)
	live, err := r.detect()
	if err != nil {
		return restoreDoneMsg{err: fmt.Errorf("restore: detect sessions: %w", err)}
	}
	actions, skipped := planRestore(snap.Sessions, panes, live, r.reg, r.exists)

	msg := restoreDoneMsg{info: info, skipped: skipped}
	for _, a := range actions {
		sess := entrySession(a.Entry)
		target := a.Pane
		if a.Kind == restoreSamePane && r.stillShell(a.Pane) {
			err = r.resumeInPane(sess, a.Pane, a.Command)
		} else {
			target, err = r.resumeInSession(sess, a.Entry.TmuxSession, a.Command)
		}
		if err != nil {
			msg.errs = append(msg.errs, fmt.Errorf("%s: %w", a.Entry.Project, err))
			continue
		}
		msg.restored++
		msg.resumed = append(msg.resumed, resumedSession{key: sess.Key(), target: target})
	}
	// Mark the snapshot as this server's so no later launch restores it again.
	_, _ = r.store.SaveIfChanged(info.SocketPath, session.LiveSnapshot{
		ServerStart: info.StartTime,
		SavedAt:     r.now(),
		Sessions:    snap.Sessions,
	})
	return msg
}

// stillShell re-checks a pane right before typing into it; the plan's listing
// may be seconds old.
func (r restorer) stillShell(target string) bool {
	cmd, err := r.paneCommand(target)
	return err == nil && isShell(cmd)
}

// waitForResurrect lets tmux-resurrect finish rebuilding the layout, so restore
// doesn't recreate sessions it is about to bring back; returns the last listing.
func waitForResurrect(r restorer, info tmux.ServerIdentity) []tmux.PaneInfo {
	var waited time.Duration
	if age := r.now().Sub(time.Unix(info.StartTime, 0)); age < restoreMinServerAge {
		r.sleep(restoreMinServerAge - age)
		waited += restoreMinServerAge - age
	}
	prev, _ := r.listPanes()
	for waited+restorePollInterval <= restoreMaxWait {
		r.sleep(restorePollInterval)
		waited += restorePollInterval
		cur, _ := r.listPanes()
		if slices.Equal(paneSignature(prev), paneSignature(cur)) {
			return cur
		}
		prev = cur
	}
	return prev
}

// paneSignature is a sorted target+command list for comparing pane listings.
func paneSignature(panes []tmux.PaneInfo) []string {
	out := make([]string, len(panes))
	for i, p := range panes {
		out[i] = p.Target + "\x00" + p.CurrentCommand
	}
	slices.Sort(out)
	return out
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// lockRestore lets one naviClaude restore at a time, across tmux servers.
func lockRestore() (func(), error) {
	path := session.DefaultLiveStatePath()
	if path == "" {
		return func() {}, nil // no cache dir: nothing is saved, so nothing to guard
	}
	return session.TryLockFile(filepath.Join(filepath.Dir(path), "restore.lock"))
}

// restoreSessionsCmd checks for sessions to restore and reports them as pending,
// before the slow wait for resurrect starts.
func (m Model) restoreSessionsCmd() tea.Cmd {
	r := m.newRestorer()
	return func() tea.Msg {
		info, n, err := r.check()
		switch {
		case err != nil:
			return restoreDoneMsg{err: err}
		case n == 0:
			return restoreDoneMsg{info: info}
		}
		return restorePendingMsg{count: n}
	}
}

// runRestoreCmd runs the startup restore off the UI goroutine.
func (m Model) runRestoreCmd() tea.Cmd {
	r := m.newRestorer()
	return func() tea.Msg { return r.run() }
}

func (m Model) newRestorer() restorer {
	return restorer{
		store:           m.liveState,
		reg:             m.registry,
		serverIdentity:  m.tmuxClient.ServerIdentity,
		listPanes:       m.tmuxClient.ListPanes,
		detect:          m.detector.Detect,
		resumeInPane:    m.manager.ResumeInPane,
		resumeInSession: m.manager.ResumeInSession,
		exists:          fileExists,
		lock:            lockRestore,
		paneCommand:     m.tmuxClient.PaneCommand,
		now:             time.Now,
		sleep:           time.Sleep,
	}
}

func restoringNotice(n int) string {
	if n == 1 {
		return styles.IconRestore + " restoring 1 session…"
	}
	return fmt.Sprintf("%s restoring %d sessions…", styles.IconRestore, n)
}

// applyRestorePending tells the user a restore is waiting for tmux-resurrect.
func (m *Model) applyRestorePending(msg restorePendingMsg) tea.Cmd {
	m.statusbar.SetNotice(restoringNotice(msg.count))
	return m.runRestoreCmd()
}

// applyRestoreDone opens the snapshot gate and reports the outcome; a failed check
// keeps the gate closed so the old snapshot survives for the next launch.
func (m *Model) applyRestoreDone(msg restoreDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.fail(fmt.Errorf("restore sessions: %w (retrying)", msg.err))
		return tea.Tick(restoreRetryDelay, func(time.Time) tea.Msg { return restoreRetryMsg{} })
	}
	m.serverIdentity = msg.info
	m.liveStateChecked = true
	now := time.Now()
	for _, r := range msg.resumed {
		r.since = now
		m.restoring = append(m.restoring, r)
	}
	switch {
	case len(msg.errs) > 0:
		m.fail(fmt.Errorf("restored %d sessions, %d failed: %w", msg.restored, len(msg.errs), msg.errs[0]))
	case msg.restored == 1:
		m.statusbar.SetNotice("restored 1 session")
	case msg.restored > 1:
		m.statusbar.SetNotice(fmt.Sprintf("restored %d sessions", msg.restored))
	}
	// The next session scan (every second) writes the snapshot; m.activeSessions
	// may still be empty here and would wipe it.
	return nil
}

// liveEntryGrace keeps a vanished session on the list this long, so a reboot
// killing agents before naviClaude, or one empty scan, can't shrink it.
const liveEntryGrace = 10 * time.Second

// liveRecorder smooths successive scans into the list that gets saved; save
// commands run concurrently, hence the mutex.
type liveRecorder struct {
	mu       sync.Mutex
	lastSeen map[string]time.Time
	entries  map[string]session.LiveEntry
}

func newLiveRecorder() *liveRecorder {
	return &liveRecorder{lastSeen: map[string]time.Time{}, entries: map[string]session.LiveEntry{}}
}

// record merges a scan into the list: new entries appear at once, missing ones
// drop out after liveEntryGrace. The result is sorted by tmux location.
func (r *liveRecorder) record(now time.Time, current []session.LiveEntry) []session.LiveEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range current {
		k := e.Harness + ":" + e.ID
		r.entries[k], r.lastSeen[k] = e, now
	}
	out := make([]session.LiveEntry, 0, len(r.entries))
	for k, e := range r.entries {
		if now.Sub(r.lastSeen[k]) > liveEntryGrace {
			delete(r.entries, k)
			delete(r.lastSeen, k)
			continue
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b session.LiveEntry) int {
		if c := strings.Compare(a.TmuxSession, b.TmuxSession); c != 0 {
			return c
		}
		if a.WindowIndex != b.WindowIndex {
			return a.WindowIndex - b.WindowIndex
		}
		if a.PaneIndex != b.PaneIndex {
			return a.PaneIndex - b.PaneIndex
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// saveLiveStateCmd records the live sessions for the next tmux restart, once the
// startup restore has consumed the previous server's snapshot.
func (m Model) saveLiveStateCmd(active []*session.Session) tea.Cmd {
	if m.liveState == nil || m.liveRecorder == nil || !m.liveStateChecked {
		return nil
	}
	store, rec, reg, info := m.liveState, m.liveRecorder, m.registry, m.serverIdentity
	active = slices.Clone(active)
	return func() tea.Msg {
		now := time.Now()
		_, _ = store.SaveIfChanged(info.SocketPath, session.LiveSnapshot{
			ServerStart: info.StartTime,
			SavedAt:     now,
			Sessions:    rec.record(now, liveEntries(active, reg, fileExists)),
		})
		return nil
	}
}

// restoringMaxAge ends the restoring state for a session that never reports a
// status of its own.
const restoringMaxAge = 60 * time.Second

// markRestoring holds resumed sessions at "restoring" until their harness reports
// a status: until then the pane heuristics can read a loading conversation as a prompt.
func (m *Model) markRestoring(active []*session.Session, now time.Time) {
	var kept []resumedSession
	for _, r := range m.restoring {
		if now.Sub(r.since) > restoringMaxAge {
			continue
		}
		var s *session.Session
		for _, a := range active {
			if a.Key() == r.key || a.TmuxTarget == r.target {
				s = a
				break
			}
		}
		if s != nil && s.NativeStatus {
			continue
		}
		kept = append(kept, r)
		if s != nil {
			s.Restoring = true
			s.Status = session.StatusIdle
			s.WaitingFor = ""
		}
	}
	m.restoring = kept
}
