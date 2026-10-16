// Package attention turns Claude Code hook events into notification sounds and
// tmux window markers. Claude's own status file is the source of truth: hooks
// only trigger a check, and a short-lived detached watcher clears the marker
// once the session stops needing the user.
package attention

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/notify"
)

// Attention values stored in the pane's @naviclaude_attn option.
const (
	Waiting = "waiting" // Claude is blocked on the user
	Done    = "done"    // a turn finished while the user was elsewhere
)

// Kind is what a hook event asks the engine to do.
type Kind string

const (
	KindIgnore    Kind = ""
	KindAttention Kind = "attention"
	KindDone      Kind = "done"
	KindClear     Kind = "clear"
)

// engine holds the injectable dependencies; tests swap them out.
type engine struct {
	tmux    tmuxAPI
	cfg     config.Notifications
	native  func(sessionID string) (claude.NativeState, bool)
	play    func(notify.Sound, string, float64) error
	now     func() time.Time
	sleep   func(time.Duration)
	lock    func() func()
	logf    func(format string, args ...any)
	poll    time.Duration // watcher poll interval
	maxLife time.Duration // watcher gives up after this long
}

func newEngine(cfg config.Notifications) *engine {
	return &engine{
		tmux:    execTmux{},
		cfg:     cfg,
		native:  func(id string) (claude.NativeState, bool) { return claude.ReadNativeState(0, id) },
		play:    notify.Play,
		now:     time.Now,
		sleep:   time.Sleep,
		lock:    fileLock,
		logf:    debugLogf,
		poll:    500 * time.Millisecond,
		maxLife: 12 * time.Hour,
	}
}

// confirm waits until Claude's own status matches want, giving the UI time to
// draw the dialog (or auto-approve it). Without a status file it trusts the
// event after the full window.
func (e *engine) confirm(sessionID string, want func(status string) bool, window time.Duration) (claude.NativeState, bool) {
	deadline := e.now().Add(window)
	var st claude.NativeState
	var ok bool
	for {
		st, ok = e.native(sessionID)
		if ok && want(st.Status) {
			return st, true
		}
		if !e.now().Before(deadline) {
			return st, !ok
		}
		e.sleep(100 * time.Millisecond)
	}
}

// Raise handles an attention or done event for pane and, when it marks the
// pane, keeps watching until the attention is resolved.
func (e *engine) Raise(kind Kind, pane, sessionID string) {
	attn, sound, want, window := Waiting, notify.Request, isWaiting, 1500*time.Millisecond
	if kind == KindDone {
		attn, sound, want, window = Done, notify.Done, isIdle, 700*time.Millisecond
	}
	st, ok := e.confirm(sessionID, want, window)
	if !ok {
		e.logf("%s %s: native status %q, not raising", kind, pane, st.Status)
		return
	}

	token := newToken()
	var ring bool
	unlock := e.lock()
	ps, err := e.tmux.Pane(pane)
	if err != nil {
		unlock()
		e.logf("%s %s: %v", kind, pane, err)
		return
	}
	visible := ps.OnScreen && e.tmux.SessionFocused(ps.Session)
	su := strconv.FormatInt(st.UpdatedAt, 10)
	switch {
	case attn == Waiting && ((st.UpdatedAt != 0 && ps.AttnSU == st.UpdatedAt) || (st.UpdatedAt == 0 && ps.Attn == Waiting)):
		// Same prompt as an earlier hook (PermissionRequest, then Notification).
		unlock()
		return
	case visible:
		// Remember the prompt we rang for so a later hook for it stays quiet.
		ring = attn == Waiting || e.cfg.DoneSoundWhenFocused
		token = ""
		_ = e.tmux.SetPaneOptions(pane, map[string]string{optAttn: "", optAttnSU: su, optToken: ""})
	default:
		ring = true
		_ = e.tmux.SetPaneOptions(pane, map[string]string{
			optAttn: attn, optAttnSU: su, optToken: token,
		})
	}
	e.syncWindow(ps.Window)
	ring = ring && e.claimRing()
	unlock()

	e.logf("%s %s: raised (visible=%v ring=%v waitingFor=%q)", kind, pane, visible, ring, st.WaitingFor)
	if ring {
		custom := e.cfg.RequestSound
		if sound == notify.Done {
			custom = e.cfg.DoneSound
		}
		if err := e.play(sound, custom, e.cfg.Volume); err != nil {
			e.logf("play %s: %v", sound, err)
		}
	}
	if token != "" {
		e.watch(pane, ps.Window, sessionID, attn, token)
	}
}

func isWaiting(status string) bool { return status == "waiting" }
func isIdle(status string) bool    { return status == "idle" || status == "shell" }

// claimRing enforces the global cooldown between sounds. Call with the lock held.
func (e *engine) claimRing() bool {
	cooldown, _ := time.ParseDuration(e.cfg.Cooldown)
	now := e.now().UnixMilli()
	if last := e.tmux.LastRing(); last > 0 && now-last < cooldown.Milliseconds() {
		return false
	}
	e.tmux.SetLastRing(now)
	return true
}

// watch polls until the attention it owns is resolved: the user looked at the
// pane, Claude moved on, another event took over, or the pane closed.
func (e *engine) watch(pane, window, sessionID, attn, token string) {
	start := e.now()
	for e.now().Sub(start) < e.maxLife {
		e.sleep(e.poll)
		ps, err := e.tmux.Pane(pane)
		if err != nil {
			// Pane closed: drop its marker from a window that may live on.
			unlock := e.lock()
			e.syncWindow(window)
			unlock()
			return
		}
		if ps.Token != token {
			return
		}
		resolved := ps.OnScreen && e.tmux.SessionFocused(ps.Session)
		if st, ok := e.native(sessionID); ok {
			switch attn {
			case Waiting:
				resolved = resolved || !isWaiting(st.Status)
			case Done:
				resolved = resolved || st.Status == "busy" || st.Status == "waiting"
			}
		}
		if resolved {
			e.clearIfOwned(pane, token)
			e.logf("watch %s: %s resolved", pane, attn)
			return
		}
	}
	e.clearIfOwned(pane, token)
}

func (e *engine) clearIfOwned(pane, token string) {
	unlock := e.lock()
	defer unlock()
	ps, err := e.tmux.Pane(pane)
	if err != nil || (token != "" && ps.Token != token) {
		return
	}
	if ps.Attn != "" || ps.Token != "" {
		_ = e.tmux.SetPaneOptions(pane, map[string]string{optAttn: "", optToken: ""})
	}
	e.syncWindow(ps.Window)
}

// Clear drops any attention on pane and re-derives its window marker.
func (e *engine) Clear(pane string) { e.clearIfOwned(pane, "") }

// syncWindow makes the window name's marker match its panes' attention:
// waiting beats done. Call with the lock held.
func (e *engine) syncWindow(window string) {
	want := ""
	if e.cfg.WindowMarker {
		attns, err := e.tmux.WindowAttns(window)
		if err != nil {
			return
		}
		for _, a := range attns {
			switch {
			case a == Waiting:
				want = e.cfg.WaitingMarker
			case a == Done && want == "":
				want = e.cfg.DoneMarker
			}
		}
	}
	ws, err := e.tmux.Window(window)
	if err != nil {
		return
	}
	applied := ws.Marker
	if applied != "" && ws.Name != markedName(ws.OrigName, applied) && ws.Name != applied+" "+ws.OrigName {
		// Renamed since we marked it: the new name is the user's, keep it.
		_ = e.tmux.SetWindowOptions(window, map[string]string{optMarker: "", optOrigName: "", optOrigAuto: ""})
		applied, ws.OrigName = "", ws.Name
	}
	if want == applied {
		return
	}
	if want == "" {
		_ = e.tmux.RenameWindow(window, ws.OrigName)
		auto := map[string]string{optMarker: "", optOrigName: "", optOrigAuto: ""}
		if ws.OrigAuto == "" {
			auto["automatic-rename"] = "" // back to inheriting the global value
		} else {
			auto["automatic-rename"] = ws.OrigAuto
		}
		_ = e.tmux.SetWindowOptions(window, auto)
		return
	}
	orig, origAuto := ws.OrigName, ws.OrigAuto
	if applied == "" {
		orig, origAuto = ws.Name, e.tmux.LocalAutoRename(window)
	}
	_ = e.tmux.SetWindowOptions(window, map[string]string{optOrigName: orig, optOrigAuto: origAuto})
	_ = e.tmux.RenameWindow(window, markedName(orig, want))
	_ = e.tmux.SetWindowOptions(window, map[string]string{optMarker: want})
}

// markedName appends the marker: a suffix needs no style reset, because tab
// formats restyle whatever follows #W, while a prefix would bleed its color.
// Names marked by older builds carried the marker as a prefix.
func markedName(name, marker string) string { return name + " " + marker }

// Clear drops the attention marker on a tmux pane id (e.g. "%12"). It is safe
// to call for panes that carry no attention.
func Clear(cfg config.Notifications, pane string) {
	if pane == "" {
		return
	}
	newEngine(cfg).Clear(pane)
}

func newToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func stateDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "naviclaude")
}

// fileLock serializes read-modify-write of tmux options across concurrent hook
// processes. It degrades to no locking if the lock file is unusable.
func fileLock() func() {
	dir := stateDir()
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "attention.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return func() {}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}

// debugLogf appends to hook.log in the state dir, only if that file already
// exists (touch it to turn logging on).
func debugLogf(format string, args ...any) {
	path := filepath.Join(stateDir(), "hook.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer f.Close()
	line := time.Now().Format("15:04:05.000") + " " + strings.TrimRight(fmt.Sprintf(format, args...), "\n") + "\n"
	_, _ = f.WriteString(line)
}
