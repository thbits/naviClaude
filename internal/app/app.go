package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/harness"
	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/preview"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
	"github.com/thbits/naviClaude/internal/tmux"
	"github.com/thbits/naviClaude/internal/ui"
	"github.com/thbits/naviClaude/internal/update"
)

// tickPreviewMsg triggers a preview capture refresh.
type tickPreviewMsg struct{}

// tickSessionMsg triggers a full session re-scan.
type tickSessionMsg struct{}

// activeSessionsMsg carries the results of the fast active-session scan.
type activeSessionsMsg struct {
	active []*session.Session
	err    error
}

// historySessionsMsg carries the results of the slower history/closed scan.
type historySessionsMsg struct {
	closed []*session.Session
	all    []*session.Session
	err    error
}

// sessionRefreshMsg carries the results of a full (combined) async session refresh.
type sessionRefreshMsg struct {
	active []*session.Session
	closed []*session.Session
	all    []*session.Session
	err    error
}

// previewCaptureMsg carries the result of an async preview capture.
type previewCaptureMsg struct {
	content       string
	target        string
	sessionID     string
	status        session.SessionStatus
	forwardsWheel bool // the pane scrolls itself on wheel events
	afterScroll   bool // captured by onPaneScrolled
	err           error
}

// closedPreviewMsg carries formatted conversation history for a closed session.
type closedPreviewMsg struct {
	content string
	err     error
}

// newSessionMsg carries the result of creating a new agent session.
type newSessionMsg struct {
	tmuxTarget  string
	tmuxSession string
	cwd         string
	harness     string
	err         error
}

// newTmuxSessionMsg carries the result of creating a new tmux session with an agent.
type newTmuxSessionMsg struct {
	tmuxTarget  string
	tmuxSession string
	cwd         string
	harness     string
	err         error
}

// resumeMsg carries a picker resume's result: the resumed pane's target and
// the closed session, whose metadata fills the placeholder until detection.
type resumeMsg struct {
	tmuxTarget  string
	tmuxSession string
	session     *session.Session
	err         error
}

// updateAvailableMsg reports that a newer naviClaude release exists on GitHub.
type updateAvailableMsg struct{ latest string }

// errMsg is a transient error notification.
type errMsg struct{ err error }

// killDoneMsg reports that a session kill finished (err is nil on success).
type killDoneMsg struct {
	target string
	err    error
}

func (e errMsg) Error() string { return e.err.Error() }

// selectionSettledMsg fires once navigation input has paused; gen identifies
// the move that scheduled it.
type selectionSettledMsg struct{ gen int }

// selectionSettleDelay is how long navigation must pause before the selected
// session's pane is resized and its details loaded.
const selectionSettleDelay = 120 * time.Millisecond

// breathingTickMsg drives the breathing animation for status dots.
type breathingTickMsg struct{}

func breathingTickCmd() tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(t time.Time) tea.Msg {
		return breathingTickMsg{}
	})
}

// Model is the top-level Bubble Tea model that composes all UI components
// and backend services.
type Model struct {
	// UI components
	sidebar      ui.SidebarModel
	rightSidebar ui.ChangedFilesModel
	preview      ui.PreviewModel
	statusbar    ui.StatusBarModel
	search       ui.SearchModel
	help         ui.HelpModel
	contextMenu  ui.ContextMenuModel
	detail       ui.DetailModel
	statsModel   ui.StatsModel
	themePicker  ui.ThemePickerModel
	nameInput    ui.NameInputModel
	renameInput  ui.NameInputModel
	dirPicker    ui.DirPickerModel
	resumePicker ui.SessionPickerModel

	// Backend services
	tmuxClient    *tmux.Client
	registry      *harness.Registry
	detector      *harness.Detector
	manager       *session.Manager
	captureEngine *preview.CaptureEngine
	passthrough   *preview.Passthrough

	// Application state
	mode                 Mode
	width, height        int
	sessions             []*session.Session // active + recently-closed (for sidebar)
	allSessions          []*session.Session // all sessions (for search)
	activeSessions       []*session.Session // just the active sessions (for progressive merge)
	err                  error              // last error, shown briefly
	confirmKill          bool               // waiting for kill confirmation
	confirmSession       *session.Session   // session pending kill confirmation
	killing              map[string]bool    // tmux targets still quitting; hidden from refreshes until done
	pendingNewTarget     string             // tmux target of a just-created session; kept until detected
	pendingNewTmuxCWD    string             // CWD for the tmux session being named
	pendingDirAction     dirAction          // which flow opened the directory picker
	pendingDirTmux       string             // target tmux session for the new-agent (n) flow
	renameSessionID      string             // session ID being renamed
	pendingResumeSession *session.Session   // closed session awaiting a resume-target choice
	pendingResumeJump    bool               // when true, jump to the resumed pane (f) instead of focusing it in passthrough (Enter)
	previewTarget        string             // tmux target currently shown in the preview panel
	resizedWinTarget     string             // window target we resized for preview
	origWinW, origWinH   int                // size to restore the previewed window to when navigating away

	// Metrics for currently selected session
	currentMetrics   *session.SessionMetrics
	metricsSessionID string // session ID the metrics belong to

	// Debounced selection sync (see deferSelectionChange).
	selectionGen     int  // bumped per deferred move; only the latest settle runs
	selectionPending bool // a settle is due; the panes still show the previous selection

	// Changed-files right sidebar
	rightPanelOpen        bool      // whether the changed-files sidebar is expanded
	changedFilesSessionID string    // session ID the loaded changed files belong to
	changedFilesMtime     time.Time // transcript mtime at last load, gates live refresh

	background backgroundMsg // session ID -> running subagents and shells

	// Session aliases (user-defined display names)
	aliasStore *session.AliasStore

	// Animation
	breathingFrame int
	spinner        spinner.Model

	// Configuration
	cfg                config.Config
	version            string // build version, for the update check
	keys               KeyMap
	sidebarWidthPct    int
	closedSessionHours float64
	currentTmuxSession string        // the tmux session naviClaude is running in
	currentTmuxWin     string        // "session:window" target of naviClaude's window
	refreshInterval    time.Duration // preview capture tick interval

	viewState           *session.ViewStateStore // persisted last-focused session + group state
	liveState           *session.LiveStateStore // sessions to resume after a tmux restart; nil when disabled
	liveStateChecked    bool                    // the startup restore finished, so the snapshot may be overwritten
	liveRecorder        *liveRecorder           // smooths scans before they overwrite the snapshot
	serverIdentity      tmux.ServerIdentity     // tmux server the snapshot is written for
	restoredLastSession bool                    // one-shot guard: restore the focused session only once at startup
	unreadHoldPane      string                  // pane set as unread while selected; kept from the seen-clear until the preview moves

	// Mouse click tracking (see mouse.go).
	lastClickID  string             // session row hit by the previous left press, for double-click
	lastClickAt  time.Time          // when that press happened
	pendingPress *mousePress        // left press on a session row awaiting release, for long-press
	copyText     func(string) error // clipboard writer; nil uses copyToClipboard
	now          func() time.Time   // clock override for tests; nil means time.Now

	// Preview scroll forwarding (see forwardWheel).
	wheelTarget           string       // pane the last capture found taking wheel events; "" scrolls locally
	scroller              paneScroller // test override; nil means tmuxClient
	scrollCaptureInFlight bool         // a post-scroll capture is running
	scrollCaptureDirty    bool         // another scroll landed during it
}

// New creates a fully-wired Model ready to be passed to tea.NewProgram.
func New(version string) Model {
	cfg, cfgErr := config.Load("")
	styles.ApplyTheme(styles.Named(cfg.Theme))
	keys := KeyMapFromConfig(cfg.Keys)

	refreshInterval, err := time.ParseDuration(cfg.RefreshInterval)
	if err != nil || refreshInterval < 50*time.Millisecond {
		refreshInterval = 200 * time.Millisecond
	}

	tc := tmux.New()
	registry := harness.NewRegistry(claude.New(claude.Options{
		Command:      cfg.ClaudeCommand,
		ProcessNames: cfg.ProcessNames,
	}))

	m := Model{
		// UI
		sidebar:      ui.NewSidebar(30, 24),
		rightSidebar: ui.NewChangedFiles(30, 24),
		preview:      ui.NewPreview(50, 24),
		statusbar:    ui.NewStatusBar(80, version),
		search:       ui.NewSearch(),
		help:         ui.NewHelp(),
		contextMenu:  ui.NewContextMenu(),
		detail:       ui.NewDetail(),
		statsModel:   ui.NewStats(),
		themePicker:  ui.NewThemePicker(cfg.Theme),
		nameInput:    ui.NewNameInput(),
		renameInput:  ui.NewRenameInput(),
		dirPicker:    ui.NewDirPicker(),
		resumePicker: ui.NewSessionPicker(),

		// Backend
		tmuxClient:    tc,
		registry:      registry,
		detector:      harness.NewDetector(tc, registry, cfg.ActiveWindowSecs, cfg.CPUActiveThreshold),
		manager:       session.NewManager(tc, cfg.WindowName),
		captureEngine: preview.NewCaptureEngine(tc),
		passthrough:   preview.NewPassthrough(tc),

		// Session aliases
		aliasStore: session.NewAliasStore(""),

		// View state (last focused session + group open/closed state)
		viewState: session.NewViewStateStore(""),

		// Configuration
		cfg:                cfg,
		version:            version,
		keys:               keys,
		mode:               ModeList,
		sidebarWidthPct:    cfg.SidebarWidth,
		closedSessionHours: cfg.ClosedSessionHours,
		currentTmuxSession: detectCurrentTmuxSession(),
		currentTmuxWin:     detectCurrentTmuxWindow(),
		refreshInterval:    refreshInterval,
	}

	ttls := make(map[string]time.Duration)
	for _, h := range registry.All() {
		ttls[h.Info().Name] = h.Info().PromptCacheTTL
	}
	ttls[""] = registry.Default().Info().PromptCacheTTL
	m.preview.SetCacheTTLs(ttls)
	m.sidebar.SetContextLimitFunc(contextLimitFunc(registry))

	// Initialize loading spinner.
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(styles.ColorBlue)
	m.spinner = s

	// Wire auto-collapse threshold and sort orders.
	m.sidebar.SetCollapseAfterHours(cfg.CollapseAfterHours)
	m.sidebar.SetGroupSortOrder(cfg.GroupSortOrder)
	m.sidebar.SetSessionSortOrder(cfg.SessionSortOrder)

	if cfg.RestoreSessions {
		m.liveState = session.NewLiveStateStore("")
		m.liveRecorder = newLiveRecorder()
	}

	// Seed persisted group open/closed state before the first SetSessions so
	// auto-collapse honors the user's remembered toggles.
	if cfg.RememberGroupState {
		m.sidebar.SeedToggledGroups(m.viewState.Get().CollapsedGroups)
	}

	// Wire dynamic key labels into help and status bar.
	helpBindings := keys.HelpBindings()
	hbi := make([]ui.HelpBindingInput, len(helpBindings))
	for i, b := range helpBindings {
		hbi[i] = ui.HelpBindingInput{Key: b.Key, Desc: b.Desc}
	}
	m.help.SetKeyBindings(hbi)

	statusHints := keys.StatusHints()
	shi := make([]ui.StatusHintInput, len(statusHints))
	for i, h := range statusHints {
		shi[i] = ui.StatusHintInput{Key: h.Key, Desc: h.Desc}
	}
	m.statusbar.SetKeyHints(shi)

	if cfgErr != nil {
		m.fail(fmt.Errorf("config: %w (using defaults)", cfgErr))
	} else if conflicts := ValidateKeys(cfg.Keys); len(conflicts) > 0 {
		msgs := make([]string, len(conflicts))
		for i, c := range conflicts {
			msgs[i] = c.String()
		}
		m.fail(fmt.Errorf("keys: %s", strings.Join(msgs, "; ")))
	}

	return m
}

// fail records err and shows it in the status bar.
func (m *Model) fail(err error) {
	m.err = err
	m.statusbar.SetError(err.Error())
}

// Init checks that tmux is available and starts the ticker commands.
// Fires the fast active-session scan first for progressive loading.
func (m Model) Init() tea.Cmd {
	if !m.tmuxClient.IsRunning() {
		fmt.Fprintln(os.Stderr, "Error: tmux is not running -- naviClaude requires an active tmux server")
		return tea.Quit
	}
	if err := m.tmuxClient.CheckVersion(3, 2); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return tea.Quit
	}
	cmds := []tea.Cmd{
		m.tickPreviewCmd(),
		tickSession(),
		tickResource(),
		m.refreshActiveCmd(),
		breathingTickCmd(),
		m.spinner.Tick,
	}
	if m.cfg.CheckForUpdates {
		cmds = append(cmds, m.checkUpdateCmd())
	}
	if m.liveState != nil {
		cmds = append(cmds, m.restoreSessionsCmd())
	}
	return tea.Batch(cmds...)
}

// checkUpdateCmd queries GitHub (cached) for a newer release off the render
// path, emitting updateAvailableMsg only when an update is available.
func (m Model) checkUpdateCmd() tea.Cmd {
	version := m.version
	return func() tea.Msg {
		latest, available := update.Check(context.Background(), version)
		if !available {
			return nil
		}
		return updateAvailableMsg{latest: latest}
	}
}

// Update is the main message router.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// -- Window resize -------------------------------------------------------
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeComponents()
		// Re-fit the previewed pane to the new viewport; the saved original
		// size is the window's, not ours, so it stays untouched.
		m.resizePreviewedPane()
		return m, nil

	// -- Tickers -------------------------------------------------------------
	case tickPreviewMsg:
		cmds = append(cmds, m.tickPreviewCmd())
		cmds = append(cmds, m.capturePreviewCmd())
		return m, tea.Batch(cmds...)

	case tickSessionMsg:
		cmds = append(cmds, tickSession())
		// The periodic ticker fires the full combined refresh.
		cmds = append(cmds, m.refreshSessionsCmd())
		// Live-refresh the changed-files panel if its session's transcript grew.
		if cmd := m.maybeRefreshChangedFilesCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.sidebarDetailCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, m.backgroundCmd())
		return m, tea.Batch(cmds...)

	case tickResourceMsg:
		cmds = append(cmds, tickResource())
		cmds = append(cmds, m.refreshResourceCmd())
		return m, tea.Batch(cmds...)

	case breathingTickMsg:
		m.breathingFrame++
		m.sidebar.SetBreathingFrame(m.breathingFrame)
		return m, breathingTickCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case resourceRefreshMsg:
		m.handleResourceRefresh(msg)
		return m, nil

	case selectionSettledMsg:
		if msg.gen != m.selectionGen || !m.selectionPending {
			return m, nil // superseded by a later move or an immediate sync
		}
		cmd := m.afterSelectionChange()
		return m, tea.Batch(cmd, m.capturePreviewCmd())

	case detailDataMsg:
		// Drop a slow read if the popup has since moved to another session.
		if sel := m.sidebar.SelectedSession(); sel != nil && sel.ID == msg.sessionID {
			m.detail.SetData(msg.messageCount, msg.startTime)
		}
		return m, nil

	case statsComputeMsg:
		// A result outliving its popup or its filter would land under the wrong badge.
		if !m.statsModel.IsVisible() {
			return m, nil
		}
		if msg.err != nil {
			m.fail(fmt.Errorf("stats: %w", msg.err))
			m.statsModel.Hide()
			m.mode = ModeList
			m.statusbar.SetMode(ModeList.String())
		} else if msg.stats != nil && msg.stats.Filter == m.statsModel.FilterString() {
			m.statsModel.SetStats(msg.stats)
		}
		return m, nil

	case metricsMsg:
		// Only apply if metrics are still for the currently selected session.
		if sel := m.sidebar.SelectedSession(); sel != nil && sel.ID == msg.sessionID {
			m.currentMetrics = msg.metrics
			m.metricsSessionID = msg.sessionID
			m.sidebar.SetMetrics(msg.sessionID, msg.metrics)
			m.preview.SetMetrics(msg.sessionID, msg.metrics)
		}
		return m, nil

	case changedFilesMsg:
		// Only apply if the changed files are still for the selected session
		// (a slow read must not repaint after the user navigated away).
		if sel := m.sidebar.SelectedSession(); sel != nil && sel.ID == msg.sessionID {
			m.changedFilesSessionID = msg.sessionID
			m.changedFilesMtime = msg.mtime
			m.rightSidebar.SetFiles(msg.files, sel.CWD)
			m.sidebar.SetChangedFiles(msg.sessionID, msg.files)
		}
		return m, nil

	case sidebarDetailMsg:
		m.applySidebarDetail(msg)
		return m, nil

	case backgroundMsg:
		m.background = msg
		m.sidebar.SetBackground(msg)
		m.preview.SetBackground(msg)
		return m, nil

	case editorDoneMsg:
		// The $EDITOR process exited. Surface a launch/exit failure; otherwise
		// there is nothing to do -- the preview tick repaints any on-disk change.
		if msg.err != nil {
			m.fail(fmt.Errorf("editor: %w", msg.err))
		}
		return m, nil

	// -- Async results (progressive: active first, then history) -------------
	case restoreDoneMsg:
		return m, m.applyRestoreDone(msg)

	case restoreRetryMsg:
		return m, m.restoreSessionsCmd()

	case activeSessionsMsg:
		var saveCmd tea.Cmd
		if msg.err != nil {
			m.err = msg.err
		} else {
			active := m.mergePendingPlaceholder(msg.active)
			m.activeSessions = active
			saveCmd = m.saveLiveStateCmd(active)
			// Pushing active-only would briefly drop closed sessions and jump the
			// cursor; the combined list is rebuilt once history arrives.
			if len(m.sessions) == 0 {
				m.sessions = active
				if m.mode != ModeSearch {
					m.sidebar.SetSessions(active)
				}
			}
			// Restore the last focused session if enabled (may be active).
			restoreCmd := m.maybeRestoreLastSession()
			if sel := m.sidebar.SelectedSession(); sel != nil {
				m.preview.SetSession(sel)
				// Fire metrics load for the initially selected session.
				if sel.ID != "" && sel.ID != m.metricsSessionID {
					m.metricsSessionID = sel.ID
					return m, tea.Batch(m.refreshHistoryCmd(), loadMetricsCmd(m.harnessFor(sel), sel), restoreCmd, saveCmd)
				}
			}
			if restoreCmd != nil {
				return m, tea.Batch(m.refreshHistoryCmd(), restoreCmd, saveCmd)
			}
		}
		// Fire the slower history scan.
		return m, tea.Batch(m.refreshHistoryCmd(), saveCmd)

	case historySessionsMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			// Merge active + closed for the sidebar.
			combined := make([]*session.Session, 0, len(m.activeSessions)+len(msg.closed))
			combined = append(combined, m.activeSessions...)
			combined = append(combined, msg.closed...)
			m.sessions = combined
			prevSel := m.selectionIdentity()
			if m.mode != ModeSearch {
				m.sidebar.SetSessions(combined)
			}

			// All sessions for search.
			allCombined := make([]*session.Session, 0, len(m.activeSessions)+len(msg.all))
			allCombined = append(allCombined, m.activeSessions...)
			allCombined = append(allCombined, msg.all...)
			m.allSessions = allCombined
			m.search.SetSessions(allCombined)

			// During search, push re-filtered results to sidebar.
			if m.mode == ModeSearch {
				m.sidebar.SetSessions(m.search.Results())
			}

			// The last focused session may be closed and appear only after the history
			// scan; if it is still absent then, the default cursor stands.
			restoreCmd := m.maybeRestoreLastSession()
			m.restoredLastSession = true
			if restoreCmd != nil {
				return m, restoreCmd
			}
			if m.selectionIdentity() != prevSel && !m.selectionPending {
				return m, m.afterSelectionChange()
			}
			if sel := m.sidebar.SelectedSession(); sel != nil {
				m.preview.SetSession(sel)
			}
		}
		return m, nil

	// -- Full combined refresh (from periodic ticker) -----------------------
	case sessionRefreshMsg:
		var cmd tea.Cmd
		if msg.err != nil {
			m.err = msg.err
		} else {
			cmd = m.applySessionRefresh(msg)
		}
		return m, tea.Batch(cmd, m.clearSeenAttentionCmd())

	case paneScrolledMsg:
		return m.onPaneScrolled(msg.target)

	case previewCaptureMsg:
		var follow tea.Cmd
		if msg.afterScroll {
			follow = m.finishScrollCapture()
		}
		// Drop results for a session the user navigated away from; match the ID too,
		// since a new session can reuse a closed one's pane target.
		if sel := m.sidebar.SelectedSession(); sel == nil || sel.TmuxTarget != msg.target || sel.ID != msg.sessionID {
			return m, follow
		}
		m.wheelTarget = ""
		if msg.forwardsWheel && msg.err == nil {
			m.wheelTarget = msg.target
			// The capture is just the visible screen; keep it pinned.
			m.preview.ResetScroll()
		}
		if msg.err != nil {
			// Pane may have disappeared; not fatal.
			m.preview.SetContent(fmt.Sprintf("  Capture failed: %s", msg.err))
		} else {
			if strings.TrimSpace(msg.content) == "" {
				// Empty pane content (session still loading or idle blank screen).
				m.preview.SetContent("  Waiting for session output...")
			} else {
				m.preview.SetContent(msg.content)
			}
			// Only Waiting comes from captures: the detector owns Active/Idle, and
			// spinners/cursor blink would make Active flicker.
			if msg.status == session.StatusWaiting {
				m.updateSessionStatus(msg.target, msg.status)
			}
		}
		return m, follow

	case newSessionMsg:
		if msg.err != nil {
			m.fail(fmt.Errorf("new session: %w", msg.err))
			return m, nil
		}
		// LastActivity=now keeps the (possibly stale) target group from
		// auto-collapsing, which would hide the new row and drop focus.
		placeholder := &session.Session{
			TmuxSession:  msg.tmuxSession,
			TmuxTarget:   msg.tmuxTarget,
			CWD:          msg.cwd,
			ProjectName:  msg.harness,
			Harness:      msg.harness,
			Status:       session.StatusActive,
			LastActivity: time.Now(),
		}
		m.enterNewSessionPassthrough(placeholder)
		// Fire a refresh so the real session (with ID etc.) replaces the placeholder.
		return m, tea.Batch(m.refreshSessionsCmd(), m.reloadMetricsForSelection(placeholder))

	case resumeMsg:
		if msg.err != nil {
			m.pendingResumeJump = false
			m.fail(msg.err)
			return m, nil
		}
		if m.pendingResumeJump {
			// f-style resume: jump to the resumed pane so it opens at full terminal
			// size, like jumping to an open session.
			m.pendingResumeJump = false
			m.persistViewState()
			m.restorePreviewedPane()
			_ = m.tmuxClient.SwitchClient(msg.tmuxTarget)
			_ = m.tmuxClient.SelectPane(msg.tmuxTarget)
			return m, tea.Quit
		}
		// Enter-style resume: focus a placeholder carrying the closed session's
		// metadata; the next refresh swaps in the detected session.
		src := msg.session
		placeholder := &session.Session{
			ID:           src.ID,
			TmuxSession:  msg.tmuxSession,
			TmuxTarget:   msg.tmuxTarget,
			CWD:          src.CWD,
			ProjectName:  src.ProjectName,
			DisplayName:  src.DisplayName,
			Slug:         src.Slug,
			Model:        src.Model,
			Harness:      src.Harness,
			SessionFile:  src.SessionFile,
			Status:       session.StatusActive,
			LastActivity: src.LastActivity,
		}
		// LastActivity keeps the closed session's time for display;
		// enterNewSessionPassthrough expands the group so the row stays visible.
		m.enterNewSessionPassthrough(placeholder)
		return m, tea.Batch(m.refreshSessionsCmd(), m.reloadMetricsForSelection(placeholder))

	case newTmuxSessionMsg:
		if msg.err != nil {
			m.fail(fmt.Errorf("new tmux session: %w", msg.err))
			return m, nil
		}
		// Create a placeholder session and select it in the sidebar.
		placeholder := &session.Session{
			TmuxSession:  msg.tmuxSession,
			TmuxTarget:   msg.tmuxTarget,
			CWD:          msg.cwd,
			ProjectName:  filepath.Base(msg.cwd),
			Harness:      msg.harness,
			Status:       session.StatusActive,
			LastActivity: time.Now(), // prevent auto-collapse of the new group
		}
		if placeholder.ProjectName == "" || placeholder.ProjectName == "." {
			placeholder.ProjectName = msg.harness
		}
		m.sessions = append([]*session.Session{placeholder}, m.sessions...)
		m.sidebar.SetSessions(m.sessions)
		// Select by target since ID is unknown yet.
		for i, item := range m.sidebar.FlatItems() {
			if item.Session != nil && item.Session.TmuxTarget == msg.tmuxTarget {
				m.sidebar.SetCursor(i)
				break
			}
		}
		m.selectPreviewSession(placeholder)
		m.pendingNewTarget = msg.tmuxTarget
		// Stay in list mode -- the user can press Enter to focus if they want.
		return m, tea.Batch(m.refreshSessionsCmd(), m.reloadMetricsForSelection(placeholder))

	case closedPreviewMsg:
		if msg.err != nil {
			m.preview.SetContent(fmt.Sprintf("  Could not load session: %s", msg.err))
		} else {
			m.preview.SetContent(msg.content)
		}
		return m, nil

	case updateAvailableMsg:
		m.statusbar.SetUpdateAvailable(true)
		return m, nil

	case errMsg:
		m.fail(msg.err)
		return m, nil

	case killDoneMsg:
		delete(m.killing, msg.target)
		if msg.err != nil {
			m.fail(msg.err)
		}
		return m, m.refreshSessionsCmd()

	// -- Key input -----------------------------------------------------------
	case tea.KeyMsg:
		return m.handleKey(msg)

	// -- Mouse input ---------------------------------------------------------
	case tea.MouseMsg:
		return m.handleMouse(msg)

	case clipboardCopiedMsg:
		if msg.err != nil {
			m.statusbar.SetError("copy failed: " + msg.err.Error())
		} else if msg.lines == 1 {
			m.statusbar.SetNotice("copied 1 line")
		} else {
			m.statusbar.SetNotice(fmt.Sprintf("copied %d lines", msg.lines))
		}
		return m, nil
	}

	// Route unmatched messages to the open picker: its candidate-load result is
	// an unexported ui type the switch above can't match.
	if m.dirPicker.IsVisible() {
		var cmd tea.Cmd
		m.dirPicker, cmd = m.dirPicker.Update(msg)
		return m, cmd
	}

	// Likewise forward unhandled messages (e.g. the text-input cursor blink) to
	// the resume picker while it is open.
	if m.resumePicker.IsVisible() {
		var cmd tea.Cmd
		m.resumePicker, cmd = m.resumePicker.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the full UI. No loading overlay -- show the layout immediately.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return m.spinner.View() + " Initializing..."
	}

	sidebarWidth := m.sidebarWidth()
	rightWidth := m.rightSidebarWidth()
	previewWidth := m.width - sidebarWidth - rightWidth

	// Render chrome first so we can measure their actual heights.
	titleBar := m.renderTitleBar()
	statusView := m.statusbar.View()
	contentHeight := m.height - lipgloss.Height(titleBar) - lipgloss.Height(statusView)
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Pass spinner view to sidebar for the loading/empty state.
	m.sidebar.SetSpinnerView(m.spinner.View())

	// Sync pane focus before rendering; the preview has no border, so its focus
	// shows via its header chip and the neighbors dimming.
	fp := m.focusedPane()
	m.sidebar.SetFocused(fp == PaneList)
	m.rightSidebar.SetFocused(fp == PaneFiles)

	// Build the sidebar column. If search or name input is active, stack
	// the input above the sidebar.
	var sidebarView string
	if m.search.IsActive() {
		searchView := m.search.View()
		searchHeight := lipgloss.Height(searchView)
		remainingHeight := contentHeight - searchHeight
		if remainingHeight < 1 {
			remainingHeight = 1
		}
		m.sidebar.SetSize(sidebarWidth-1, remainingHeight)
		sidebarView = lipgloss.JoinVertical(lipgloss.Left, searchView, m.sidebar.View())
	} else if m.nameInput.IsActive() {
		inputView := m.nameInput.View()
		inputHeight := lipgloss.Height(inputView)
		remainingHeight := contentHeight - inputHeight
		if remainingHeight < 1 {
			remainingHeight = 1
		}
		m.sidebar.SetSize(sidebarWidth-1, remainingHeight)
		sidebarView = lipgloss.JoinVertical(lipgloss.Left, inputView, m.sidebar.View())
	} else if m.renameInput.IsActive() {
		inputView := m.renameInput.View()
		inputHeight := lipgloss.Height(inputView)
		remainingHeight := contentHeight - inputHeight
		if remainingHeight < 1 {
			remainingHeight = 1
		}
		m.sidebar.SetSize(sidebarWidth-1, remainingHeight)
		sidebarView = lipgloss.JoinVertical(lipgloss.Left, inputView, m.sidebar.View())
	} else {
		m.sidebar.SetSize(sidebarWidth-1, contentHeight)
		sidebarView = m.sidebar.View()
	}

	previewView := m.preview.View()

	// The sidebar's right border separates the panes and lights up on list focus.
	sidebarStyle := styles.SidebarPanel
	if fp == PaneList {
		sidebarStyle = styles.SidebarPanelFocused
	}
	sidebarView = sidebarStyle.
		Width(sidebarWidth - 1). // -1 for the border character
		Height(contentHeight).
		MaxHeight(contentHeight).
		Render(sidebarView)

	// Preview has NO border -- the separator is the sidebar's right border.
	previewView = lipgloss.NewStyle().
		Width(previewWidth).
		Height(contentHeight).
		MaxHeight(contentHeight).
		Render(previewView)

	columns := []string{sidebarView, previewView}

	// The changed-files sidebar sits to the right of the preview, using a LEFT
	// border as its separator (mirror of the left sidebar's right border).
	if rightWidth > 0 {
		m.rightSidebar.SetSize(rightWidth-1, contentHeight)
		rightStyle := styles.RightSidebarPanel
		if fp == PaneFiles {
			rightStyle = styles.RightSidebarPanelFocused
		}
		rightView := rightStyle.
			Width(rightWidth - 1). // -1 for the border character
			Height(contentHeight).
			MaxHeight(contentHeight).
			Render(m.rightSidebar.View())
		columns = append(columns, rightView)
	}

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, columns...)

	screen := lipgloss.JoinVertical(lipgloss.Left, titleBar, mainContent, statusView)

	// Render overlays on top, composited over the main window.
	if m.statsModel.IsVisible() {
		screen = ui.PlaceOverlay(screen, m.statsModel.View())
	}

	if m.themePicker.IsVisible() {
		screen = ui.PlaceOverlay(screen, m.themePicker.View())
	}

	if m.dirPicker.IsVisible() {
		screen = ui.PlaceOverlay(screen, m.dirPicker.View())
	}

	if m.resumePicker.IsVisible() {
		screen = ui.PlaceOverlay(screen, m.resumePicker.View())
	}

	if m.detail.IsVisible() {
		screen = ui.PlaceOverlay(screen, m.detail.View())
	}

	if m.help.IsVisible() {
		screen = ui.PlaceOverlay(screen, m.help.View())
	}

	if m.contextMenu.IsVisible() {
		x, y, _, _ := m.contextMenu.Rect()
		screen = ui.PlaceOverlayAt(screen, m.contextMenu.View(), x, y)
	}

	return screen
}

// ---------------------------------------------------------------------------
// Key handling per mode
// ---------------------------------------------------------------------------

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.statusbar.ClearError()
	// A key ends a drag whose release never arrived, unfreezing the preview.
	m.preview.ClearSelection()
	// Any keystroke puts the user in control, so cancel a pending history-driven
	// restore that would otherwise steal focus.
	m.restoredLastSession = true
	key := msg.String()

	// Global quit — but in passthrough mode Ctrl+C is forwarded to the pane.
	if key == KeyCtrlC && m.mode != ModePassthrough {
		m.persistViewState()
		m.restorePreviewedPane()
		return m, tea.Quit
	}

	// Kill confirmation takes priority.
	if m.confirmKill {
		return m.handleKillConfirm(msg)
	}

	switch m.mode {
	case ModeList:
		return m.handleListKey(msg)
	case ModePassthrough:
		return m.handlePassthroughKey(msg)
	case ModeSearch:
		return m.handleSearchKey(msg)
	case ModeContextMenu:
		return m.handleContextMenuKey(msg)
	case ModeHelp:
		return m.handleHelpKey(msg)
	case ModeDetail:
		return m.handleDetailKey(msg)
	case ModeStats:
		return m.handleStatsKey(msg)
	case ModeThemePicker:
		return m.handleThemePickerKey(msg)
	case ModeNameInput:
		return m.handleNameInputKey(msg)
	case ModeRenameSession:
		return m.handleRenameSessionKey(msg)
	case ModeDirPicker:
		return m.handleDirPickerKey(msg)
	case ModeResumePicker:
		return m.handleResumePickerKey(msg)
	case ModeChangedFiles:
		return m.handleChangedFilesKey(msg)
	}
	return m, nil
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case m.keys.Quit:
		m.persistViewState()
		m.restorePreviewedPane()
		return m, tea.Quit

	case m.keys.Help:
		m.help.Toggle()
		if m.help.IsVisible() {
			m.mode = ModeHelp
			m.statusbar.SetMode(ModeHelp.String())
		}
		return m, nil

	case m.keys.Search:
		m.mode = ModeSearch
		m.search.SetSessions(m.allSessions)
		m.search.Activate()
		m.statusbar.SetMode(ModeSearch.String())
		// Push initial (unfiltered) results to sidebar immediately so
		// active sessions don't linger from the previous full list.
		m.sidebar.SetSessions(m.search.Results())
		m.sidebar.ExpandAll() // show all groups including Closed during search
		return m, nil

	case KeyEnter:
		sess := m.sidebar.SelectedSession()
		if sess == nil {
			// Cursor is on a group header; delegate to sidebar for collapse/expand.
			var cmd tea.Cmd
			m.sidebar, cmd = m.sidebar.Update(msg)
			return m, cmd
		}
		if sess.Status == session.StatusClosed {
			// Enter resumes a closed session: open the target picker, then
			// focus the resumed session in passthrough.
			return m.openResumePicker(sess, false)
		}
		// Enter passthrough mode for active sessions.
		return m, m.setFocus(ModePassthrough)

	case KeyTab, KeyCyclePanes:
		// Tab / Shift+Tab cycle focus across the visible panes (list, preview
		// passthrough, changed-files) rather than only toggling passthrough.
		return m, m.setFocus(m.nextFocusMode(key == KeyTab))

	case m.keys.Jump:
		sess := m.sidebar.SelectedSession()
		if sess != nil && sess.Status == session.StatusClosed {
			// Resume via the picker, then jump to the resumed window rather than
			// focusing it in passthrough.
			return m.openResumePicker(sess, true)
		}
		return m.jumpToPane()

	case m.keys.KillSession:
		return m.killSelected()

	case m.keys.RenameSession:
		return m.startRenameSession()

	case m.keys.NewSession:
		return m.createNewSession()

	case m.keys.NewTmuxSession:
		return m.createNewTmuxSession()

	case m.keys.Detail:
		sess := m.sidebar.SelectedSession()
		if sess == nil {
			return m, nil
		}
		m.detail.Show(sess)
		m.mode = ModeDetail
		m.statusbar.SetMode(ModeDetail.String())
		return m, loadDetailDataCmd(m.harnessFor(sess), sess)

	case m.keys.Stats:
		m.statsModel.Show()
		m.mode = ModeStats
		m.statusbar.SetMode(ModeStats.String())
		return m, m.computeStatsCmd()

	case m.keys.ToggleChanged:
		// Opens the panel if closed, or re-focuses it if already open.
		sel := m.sidebar.SelectedSession()
		if sel == nil {
			return m, nil
		}
		wasOpen := m.rightPanelOpen
		m.rightPanelOpen = true
		cmd := m.setFocus(ModeChangedFiles)
		if !wasOpen {
			// Only the open transition changes the layout width.
			m.resizeComponents()
			m.resizePreviewedPane()
		}
		return m, cmd

	case KeyThemePicker:
		m.themePicker.Show(m.cfg.Theme)
		m.mode = ModeThemePicker
		m.statusbar.SetMode(ModeThemePicker.String())
		return m, nil

	case "ctrl+u":
		if cmd, ok := m.forwardPage(true); ok {
			return m, cmd
		}
		m.preview.ScrollUp(m.preview.HalfViewHeight())
		return m, nil

	case "ctrl+d":
		if cmd, ok := m.forwardPage(false); ok {
			return m, cmd
		}
		m.preview.ScrollDown(m.preview.HalfViewHeight())
		return m, nil

	default:
		// Navigation keys: delegate to sidebar.
		prev := m.selectionIdentity()
		var cmd tea.Cmd
		m.sidebar, cmd = m.sidebar.Update(msg)
		return m, tea.Batch(cmd, m.selectionMoved(prev))
	}
}

// selectionMoved syncs the panes after a navigation step. A move to another
// row is deferred so key repeats and wheel flicks pay for one sync, not one each.
func (m *Model) selectionMoved(prev string) tea.Cmd {
	if m.selectionIdentity() != prev {
		return m.deferSelectionChange()
	}
	if m.selectionPending {
		return nil
	}
	return m.afterSelectionChange()
}

// deferSelectionChange schedules afterSelectionChange for when input settles;
// the sidebar cursor has already repainted.
func (m *Model) deferSelectionChange() tea.Cmd {
	m.selectionGen++
	m.selectionPending = true
	gen := m.selectionGen
	return tea.Tick(selectionSettleDelay, func(time.Time) tea.Msg { return selectionSettledMsg{gen: gen} })
}

// afterSelectionChange syncs the preview, metrics, and changed-files panel to
// the sidebar cursor after it moved (keyboard or mouse).
func (m *Model) afterSelectionChange() tea.Cmd {
	m.selectionPending = false
	var cmds []tea.Cmd
	if sel := m.sidebar.SelectedSession(); sel != nil {
		m.selectPreviewSession(sel)
		// Fire metrics load if selection changed.
		if cmd := m.reloadMetricsForSelection(sel); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.reloadChangedFilesForSelection(sel); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Auto-load conversation preview for closed sessions.
		if sel.Status == session.StatusClosed {
			cmds = append(cmds, m.loadClosedPreviewCmd(sel))
		}
	} else if groupName := m.sidebar.SelectedGroupName(); groupName != "" {
		// Navigated to a group header -- restore any resized pane.
		m.selectPreviewSession(nil)
		// Show group summary when hovering on a group header.
		m.preview.SetGroupSummary(groupName, m.sidebar.GroupSessions(groupName))
		// No session selected: clear the changed-files list.
		m.rightSidebar.Reset()
		m.changedFilesSessionID = ""
	}
	return tea.Batch(cmds...)
}

// handleChangedFilesKey handles keys while the changed-files sidebar has focus.
// The panel is non-modal: moving focus away leaves it open, tracking the selection.
func (m Model) handleChangedFilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case m.keys.Quit:
		// The quit key still quits from the focused panel (matches list mode).
		m.persistViewState()
		m.restorePreviewedPane()
		return m, tea.Quit

	case m.keys.ToggleChanged:
		// Collapse the panel entirely and return to the session list.
		m.rightPanelOpen = false
		cmd := m.setFocus(ModeList)
		m.resizeComponents()
		m.resizePreviewedPane()
		return m, cmd

	case KeyMenuCancel:
		// Esc drops focus but keeps the panel open; width is unchanged.
		return m, m.setFocus(ModeList)

	case KeyTab, KeyCyclePanes:
		// Tab / Shift+Tab cycle to the next / previous pane (panel stays open).
		return m, m.setFocus(m.nextFocusMode(key == KeyTab))

	case KeyEnter, "e":
		return m, m.openInEditorCmd(m.rightSidebar.SelectedFile())

	default:
		var cmd tea.Cmd
		m.rightSidebar, cmd = m.rightSidebar.Update(msg)
		return m, cmd
	}
}

func (m Model) handlePassthroughKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case KeyExitPassthrough:
		// Ctrl+] always drops straight back to the session list.
		return m, m.setFocus(ModeList)

	case KeyCyclePanes:
		// Only Shift+Tab leaves; plain Tab is forwarded so it keeps working in the agent.
		return m, m.setFocus(m.nextFocusMode(false))

	case KeyJumpFromPT:
		return m.jumpToPane()

	default:
		// Any keystroke in passthrough snaps preview back to bottom.
		m.preview.ResetScroll()
		// Forward the key to the selected session's tmux pane.
		sess := m.sidebar.SelectedSession()
		if sess == nil || sess.TmuxTarget == "" {
			// No active session to forward to; exit passthrough.
			m.mode = ModeList
			m.preview.SetPassthrough(false)
			m.pendingNewTarget = ""
			m.statusbar.SetMode(ModeList.String())
			m.statusbar.SetError("session ended — returned to list mode")
			return m, nil
		}
		if err := m.passthrough.SendKey(sess.TmuxTarget, msg); err != nil {
			// Send-keys failed; session may have died.
			m.mode = ModeList
			m.preview.SetPassthrough(false)
			m.pendingNewTarget = ""
			m.statusbar.SetMode(ModeList.String())
			m.statusbar.SetError("session ended — returned to list mode")
			return m, nil
		}
		// Eager capture: refresh preview immediately after sending a key
		// so the user sees the result without waiting for the next tick.
		return m, m.capturePreviewCmd()
	}
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		// Clear search, restore full list, return to list mode.
		m.search.Deactivate()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		m.sidebar.SetSessions(m.sessions)
		m.sidebar.RestoreCollapsed()
		return m, nil

	case "enter":
		// Act on the selected session (same as list-mode Enter).
		sess := m.sidebar.SelectedSession()
		m.search.Deactivate()
		m.sidebar.SetSessions(m.sessions)
		m.sidebar.RestoreCollapsed()
		if sess == nil {
			// On a group header -- toggle collapse, stay in list mode.
			m.mode = ModeList
			m.statusbar.SetMode(ModeList.String())
			return m, nil
		}
		if sess.Status == session.StatusClosed {
			// Show conversation history in preview.
			m.mode = ModeList
			m.statusbar.SetMode(ModeList.String())
			m.selectSessionInSidebar(sess)
			m.selectPreviewSession(sess)
			return m, m.loadClosedPreviewCmd(sess)
		}
		// Select and enter passthrough for active sessions.
		m.selectSessionInSidebar(sess)
		m.unreadHoldPane = ""
		m.mode = ModePassthrough
		m.preview.SetPassthrough(true)
		m.statusbar.SetMode(ModePassthrough.String())
		return m, nil

	case "up", "ctrl+p", "down", "ctrl+n":
		// Navigate the filtered results, synced like list-mode navigation.
		prev := m.selectionIdentity()
		var cmd tea.Cmd
		m.sidebar, cmd = m.sidebar.Update(msg)
		return m, tea.Batch(cmd, m.selectionMoved(prev))

	default:
		// All other keys go to the text input for filtering.
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		// Update sidebar with filtered results.
		m.sidebar.SetSessions(m.search.Results())
		return m, cmd
	}
}

func (m Model) handleNameInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.nameInput.Deactivate()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m, nil

	case "enter":
		name := m.nameInput.Value()
		m.nameInput.Deactivate()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m.confirmNewTmuxSession(name)

	default:
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		return m, cmd
	}
}

func (m Model) startRenameSession() (tea.Model, tea.Cmd) {
	sess := m.sidebar.SelectedSession()
	if sess == nil {
		return m, nil
	}
	m.renameSessionID = sess.Key()
	m.mode = ModeRenameSession
	m.renameInput.SetSize(m.sidebarWidth())
	// Pre-fill with current display name.
	current := sess.DisplayName
	if current == "" {
		current = sess.Slug
	}
	cmd := m.renameInput.ActivateWithValue(current)
	m.statusbar.SetMode(ModeRenameSession.String())
	return m, cmd
}

func (m Model) handleRenameSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.renameInput.Deactivate()
		m.renameSessionID = ""
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m, nil

	case "enter":
		name := m.renameInput.Value()
		m.renameInput.Deactivate()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())

		if m.renameSessionID != "" {
			if err := m.aliasStore.Set(m.renameSessionID, name); err != nil {
				m.statusbar.SetError("rename failed: " + err.Error())
			}
			// Apply the updated alias set to current sessions immediately.
			m.applyAliases(m.sessions)
			m.applyAliases(m.allSessions)
			m.sidebar.SetSessions(m.sessions)
		}
		m.renameSessionID = ""
		return m, nil

	default:
		var cmd tea.Cmd
		m.renameInput, cmd = m.renameInput.Update(msg)
		return m, cmd
	}
}

func (m Model) handleContextMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case KeyMenuSelect:
		return m.runContextMenuSelection()

	case KeyMenuCancel:
		m.closeContextMenu()
		return m, nil

	default:
		var cmd tea.Cmd
		m.contextMenu, cmd = m.contextMenu.Update(msg)
		// If the context menu closed itself (e.g., Esc handled internally):
		if !m.contextMenu.IsVisible() {
			m.mode = ModeList
			m.statusbar.SetMode(ModeList.String())
		}
		return m, cmd
	}
}

// runContextMenuSelection closes the menu and runs its highlighted action.
func (m Model) runContextMenuSelection() (tea.Model, tea.Cmd) {
	action := m.contextMenu.SelectedAction()
	target := m.contextMenu.Session()
	m.closeContextMenu()
	return m.executeContextAction(action, target)
}

func (m *Model) closeContextMenu() {
	m.contextMenu.Hide()
	m.mode = ModeList
	m.statusbar.SetMode(ModeList.String())
}

func (m Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.help, cmd = m.help.Update(msg)
	if !m.help.IsVisible() {
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
	}
	return m, cmd
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

func (m Model) jumpToPane() (tea.Model, tea.Cmd) {
	sess := m.sidebar.SelectedSession()
	if sess == nil || sess.TmuxTarget == "" {
		return m, nil
	}
	if sess.Attention != "" {
		attention.Clear(m.cfg.Notifications, sess.TmuxPaneID)
	}
	return m.switchAway(sess.TmuxTarget)
}

// switchAway hands the tmux client to target and quits naviClaude.
func (m Model) switchAway(target string) (tea.Model, tea.Cmd) {
	m.persistViewState()
	m.restorePreviewedPane()
	_ = m.tmuxClient.SwitchClient(target)
	_ = m.tmuxClient.SelectPane(target)
	return m, tea.Quit
}

// openInEditor opens sess's folder in the editor. A GUI editor just launches; a
// terminal one gets a new window in the session's tmux session (naviClaude's own
// for a closed one), and the client switches there.
func (m Model) openInEditor(sess *session.Session) (tea.Model, tea.Cmd) {
	if err := checkDir(sess.CWD); err != nil {
		m.statusbar.SetError(err.Error())
		return m, nil
	}
	fields := resolveEditorFields(m.cfg.Editor)
	if isGUIEditor(fields[0]) {
		cmd := exec.Command(fields[0], append(fields[1:], sess.CWD)...)
		cmd.Dir = sess.CWD
		return m, func() tea.Msg { return editorDoneMsg{err: cmd.Run()} }
	}
	quoted := make([]string, 0, len(fields)+1)
	for _, f := range append(fields, sess.CWD) {
		quoted = append(quoted, tmux.ShellQuote(f))
	}
	tmuxSession := sess.TmuxSession
	if sess.Status == session.StatusClosed || tmuxSession == "" {
		tmuxSession = m.currentTmuxSession
	}
	if tmuxSession == "" {
		m.statusbar.SetError("cannot determine the tmux session")
		return m, nil
	}
	target, err := m.tmuxClient.NewWindowPrint(tmux.NewWindowOptions{
		Target:  tmuxSession + ":",
		Name:    sess.ProjectName,
		Command: fmt.Sprintf("cd %s && %s", tmux.ShellQuote(sess.CWD), strings.Join(quoted, " ")),
	})
	if err != nil {
		m.statusbar.SetError("open in editor: " + err.Error())
		return m, nil
	}
	return m.switchAway(target)
}

// openFolder reveals dir in the desktop file manager.
func openFolder(dir string) error {
	if err := checkDir(dir); err != nil {
		return err
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	cmd := exec.Command(opener, dir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", opener, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func checkDir(dir string) error {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("folder not found: %s", dir)
	}
	return nil
}

func (m Model) killSelected() (tea.Model, tea.Cmd) {
	sess := m.sidebar.SelectedSession()
	if sess == nil || sess.Status == session.StatusClosed {
		return m, nil
	}
	// Show inline kill confirmation on the sidebar item.
	m.confirmKill = true
	m.statusbar.SetConfirmKill(true)
	m.confirmSession = sess
	m.sidebar.ConfirmKillTarget = sess.TmuxTarget
	return m, nil
}

func (m Model) handleKillConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "y", "Y":
		var cmd tea.Cmd
		if m.confirmSession != nil {
			cmd = m.killSessionCmd(m.confirmSession)
		}
		m.confirmKill = false
		m.statusbar.SetConfirmKill(false)
		m.confirmSession = nil
		m.sidebar.ConfirmKillTarget = ""
		return m, cmd
	default:
		// Any other key cancels.
		m.confirmKill = false
		m.statusbar.SetConfirmKill(false)
		m.confirmSession = nil
		m.sidebar.ConfirmKillTarget = ""
		return m, nil
	}
}

// killSessionCmd hides sess at once and kills it off the event loop; exit hooks
// can take seconds, so refreshes skip it until killDoneMsg arrives.
func (m *Model) killSessionCmd(sess *session.Session) tea.Cmd {
	target := sess.TmuxTarget
	if m.killing == nil {
		m.killing = map[string]bool{}
	}
	m.killing[target] = true
	m.removeSession(target)
	manager := m.manager
	return func() tea.Msg {
		if err := manager.Kill(sess); err != nil {
			return killDoneMsg{target: target, err: fmt.Errorf("kill: %w", err)}
		}
		return killDoneMsg{target: target}
	}
}

// forkResumeCmd runs a harness fork-resume off the event loop, emitting an
// errMsg on failure.
func (m Model) forkResumeCmd(sess *session.Session, target string) tea.Cmd {
	return m.runResumeCmd(sess, target, true, "fork-resume")
}

// resumeCmd runs a harness resume off the event loop, emitting an errMsg on
// failure.
func (m Model) resumeCmd(sess *session.Session, target string) tea.Cmd {
	return m.runResumeCmd(sess, target, false, "resume")
}

func (m Model) runResumeCmd(sess *session.Session, target string, fork bool, label string) tea.Cmd {
	manager := m.manager
	h := m.harnessFor(sess)
	return func() tea.Msg {
		cmd, err := resumeCommandFor(h, sess, fork)
		if err == nil {
			err = manager.Resume(sess, target, cmd)
		}
		if err != nil {
			return errMsg{err: fmt.Errorf("%s: %w", label, err)}
		}
		return nil
	}
}

func (m Model) resumeSession(sess *session.Session) (tea.Model, tea.Cmd) {
	if !m.menuCaps(sess).CanResume {
		return m, nil
	}
	target := m.currentTmuxSession
	if target == "" {
		m.err = fmt.Errorf("resume: cannot determine current tmux session")
		m.statusbar.SetError("cannot determine current tmux session")
		return m, nil
	}
	// Resume off the event loop and refresh at once; passthrough waits until
	// the new pane is detected.
	return m, tea.Batch(m.resumeCmd(sess, target), m.refreshSessionsCmd())
}

// openResumePicker opens the resume-target picker for a closed session. With
// jump the resumed pane is jumped to; otherwise it is focused in passthrough.
func (m Model) openResumePicker(sess *session.Session, jump bool) (tea.Model, tea.Cmd) {
	if !m.menuCaps(sess).CanResume {
		return m, nil
	}
	names := sessionNamesByRecency(m.tmuxClient.ListSessions())
	m.pendingResumeSession = sess
	m.pendingResumeJump = jump
	m.resumePicker.SetSize(m.width, m.height)
	m.resumePicker.Show(names, m.currentTmuxSession)
	m.mode = ModeResumePicker
	m.statusbar.SetMode(ModeResumePicker.String())
	return m, m.resumePicker.Init()
}

// handleResumePickerKey drives the resume-target picker; Enter resumes into the
// chosen (or typed-new) tmux session.
func (m Model) handleResumePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.resumePicker.Hide()
		m.pendingResumeSession = nil
		m.pendingResumeJump = false
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m, nil

	case "up", "ctrl+p":
		m.resumePicker.MoveUp()
		return m, nil

	case "down", "ctrl+n":
		m.resumePicker.MoveDown()
		return m, nil

	case "enter":
		name, _ := m.resumePicker.Selected()
		sess := m.pendingResumeSession
		m.resumePicker.Hide()
		m.pendingResumeSession = nil
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		if sess == nil || name == "" {
			return m, nil
		}
		// Resume off the event loop. The resulting resumeMsg places a
		// placeholder for the new pane and enters passthrough on it.
		return m, m.resumeInSessionCmd(sess, name)

	default:
		var cmd tea.Cmd
		m.resumePicker, cmd = m.resumePicker.Update(msg)
		return m, cmd
	}
}

// resumeInSessionCmd runs manager.ResumeInSession off the event loop, reporting
// the resumed pane's target in a resumeMsg.
func (m Model) resumeInSessionCmd(sess *session.Session, targetName string) tea.Cmd {
	manager := m.manager
	h := m.harnessFor(sess)
	return func() tea.Msg {
		cmd, err := resumeCommandFor(h, sess, false)
		if err != nil {
			return resumeMsg{err: fmt.Errorf("resume: %w", err)}
		}
		target, err := manager.ResumeInSession(sess, targetName, cmd)
		if err != nil {
			return resumeMsg{err: fmt.Errorf("resume: %w", err)}
		}
		return resumeMsg{tmuxTarget: target, tmuxSession: targetName, session: sess}
	}
}

// sessionNamesByRecency returns the tmux session names ordered by most-recent
// activity first.
func sessionNamesByRecency(infos []tmux.SessionInfo) []string {
	sort.SliceStable(infos, func(i, j int) bool {
		return infos[i].Activity > infos[j].Activity
	})
	names := make([]string, len(infos))
	for i, s := range infos {
		names[i] = s.Name
	}
	return names
}

// resolveNewSessionTarget picks the tmux session and cwd for a new agent session:
// the hovered session (closed ones have no tmux session, so use the current one),
// else the selected group header (named after its tmux session), else our own.
func (m Model) resolveNewSessionTarget() (tmuxSess, cwd string) {
	if sel := m.sidebar.SelectedSession(); sel != nil {
		if sel.TmuxSession != "" {
			return sel.TmuxSession, sel.CWD
		}
		return m.currentTmuxSession, sel.CWD
	}

	if name := m.sidebar.SelectedGroupName(); name != "" && name != ui.ClosedGroupName {
		if sessions := m.sidebar.GroupSessions(name); len(sessions) > 0 {
			cwd = sessions[0].CWD
		}
		return name, cwd
	}

	return m.currentTmuxSession, ""
}

func (m Model) createNewSession() (tea.Model, tea.Cmd) {
	tmuxSess, cwd := m.resolveNewSessionTarget()

	if tmuxSess == "" {
		m.err = fmt.Errorf("new session: cannot determine target tmux session")
		m.statusbar.SetError("cannot determine target tmux session")
		return m, nil
	}

	// The agent window is created once a directory is chosen; the current dir is
	// preselected so Enter opens in the highlighted session's directory.
	base := cwd
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = home
		}
	}
	m.pendingDirAction = dirActionNewAgent
	m.pendingDirTmux = tmuxSess
	return m.openDirPicker(base, "New "+m.defaultInfo().Label+" session directory")
}

// createAgentSessionIn launches a new default-harness window in tmuxSess rooted at cwd.
func (m Model) createAgentSessionIn(cwd, tmuxSess string) (tea.Model, tea.Cmd) {
	if tmuxSess == "" {
		m.err = fmt.Errorf("new session: cannot determine target tmux session")
		m.statusbar.SetError("cannot determine target tmux session")
		return m, nil
	}
	manager := m.manager
	h := m.registry.Default()
	return m, func() tea.Msg {
		target, err := manager.CreateNewWithTarget(cwd, tmuxSess, h.StartCommand(), h.Info().Name)
		if err != nil {
			return newSessionMsg{err: err}
		}
		return newSessionMsg{
			tmuxTarget:  target,
			tmuxSession: tmuxSess,
			cwd:         cwd,
			harness:     h.Info().Name,
		}
	}
}

func (m Model) createNewTmuxSession() (tea.Model, tea.Cmd) {
	// The configured directory (or home) seeds the picker, which expands ~; the
	// name input follows (see confirmDirSelection).
	cwd := m.cfg.NewSessionDir
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	m.pendingDirAction = dirActionNewTmux
	return m.openDirPicker(cwd, "New tmux session directory")
}

// openDirPicker shows the directory picker rooted at base; candidates load off
// the Update goroutine so slow zoxide/subdir lookups don't freeze the UI.
func (m Model) openDirPicker(base, title string) (tea.Model, tea.Cmd) {
	m.mode = ModeDirPicker
	m.dirPicker.SetSize(m.width, m.height)
	m.dirPicker.SetTitle(title)
	loadCmd := m.dirPicker.Show(base, m.sessionDirs())
	m.statusbar.SetMode(ModeDirPicker.String())
	return m, tea.Batch(m.dirPicker.Init(), loadCmd)
}

// sessionDirs returns the de-duplicated working directories of known sessions,
// used to seed the directory picker's candidate list.
func (m Model) sessionDirs() []string {
	seen := make(map[string]bool)
	var dirs []string
	for _, s := range m.sessions {
		if s.CWD != "" && !seen[s.CWD] {
			seen[s.CWD] = true
			dirs = append(dirs, s.CWD)
		}
	}
	return dirs
}

// handleDirPickerKey drives the directory picker: typing filters, arrows/Tab
// navigate, Enter selects, Esc cancels.
func (m Model) handleDirPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.dirPicker.Hide()
		m.pendingDirAction = dirActionNone
		m.pendingDirTmux = ""
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m, nil

	case "up", "ctrl+p":
		m.dirPicker.MoveUp()
		return m, nil

	case "down", "ctrl+n":
		m.dirPicker.MoveDown()
		return m, nil

	case "right", "tab":
		return m, m.dirPicker.Descend()

	case "left":
		return m, m.dirPicker.Parent()

	case "enter":
		dir := m.dirPicker.Selected()
		m.dirPicker.Hide()
		return m.confirmDirSelection(dir)

	default:
		var cmd tea.Cmd
		m.dirPicker, cmd = m.dirPicker.Update(msg)
		return m, cmd
	}
}

// confirmDirSelection hands the chosen directory to the flow that opened the
// picker: a new agent window (n) or the new tmux session's name input (N).
func (m Model) confirmDirSelection(dir string) (tea.Model, tea.Cmd) {
	action := m.pendingDirAction
	m.pendingDirAction = dirActionNone

	if dir == "" {
		m.pendingDirTmux = ""
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m, nil
	}

	switch action {
	case dirActionNewAgent:
		tmuxSess := m.pendingDirTmux
		m.pendingDirTmux = ""
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m.createAgentSessionIn(dir, tmuxSess)

	case dirActionNewTmux:
		m.pendingNewTmuxCWD = dir
		m.mode = ModeNameInput
		m.nameInput.SetSize(m.sidebarWidth())
		cmd := m.nameInput.Activate()
		m.statusbar.SetMode(ModeNameInput.String())
		return m, cmd

	default:
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m, nil
	}
}

func (m Model) confirmNewTmuxSession(name string) (tea.Model, tea.Cmd) {
	cwd := m.pendingNewTmuxCWD
	manager := m.manager
	h := m.registry.Default()
	sessionName := strings.TrimSpace(name)
	return m, func() tea.Msg {
		tmuxSess, target, err := manager.CreateNewTmuxSession(cwd, h.StartCommand(), sessionName, h.Info().Name)
		if err != nil {
			return newTmuxSessionMsg{err: err}
		}
		return newTmuxSessionMsg{
			tmuxTarget:  target,
			tmuxSession: tmuxSess,
			cwd:         cwd,
			harness:     h.Info().Name,
		}
	}
}

func (m Model) executeContextAction(action string, sess *session.Session) (tea.Model, tea.Cmd) {
	if sess == nil {
		return m, nil
	}
	// Each case sets its full mode/statusbar state rather than relying on the
	// caller having reset to ModeList first.
	switch action {
	case "focus":
		if sess.Status == session.StatusClosed {
			return m.resumeSession(sess)
		}
		m.unreadHoldPane = ""
		m.mode = ModePassthrough
		m.preview.SetPassthrough(true)
		m.statusbar.SetMode(ModePassthrough.String())
		return m, nil

	case "mark_unread":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		if sess.TmuxPaneID == "" {
			return m, nil
		}
		m.unreadHoldPane = sess.TmuxPaneID
		sess.Attention = attention.Done // show it now; the next refresh reads it back from tmux
		cfg, pane := m.cfg.Notifications, sess.TmuxPaneID
		return m, func() tea.Msg {
			attention.MarkDone(cfg, pane)
			return nil
		}

	case "jump":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		m.selectSessionInSidebar(sess)
		return m.jumpToPane()

	case "kill":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		cmd := m.killSessionCmd(sess)
		return m, cmd

	case "resume":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m.resumeSession(sess)

	case "fork_resume":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		target := m.currentTmuxSession
		if target == "" {
			m.err = fmt.Errorf("fork-resume: cannot determine current tmux session")
			m.statusbar.SetError("cannot determine current tmux session")
			return m, nil
		}
		return m, tea.Batch(m.forkResumeCmd(sess, target), m.refreshSessionsCmd())

	case "detail":
		m.detail.Show(sess)
		m.mode = ModeDetail
		m.statusbar.SetMode(ModeDetail.String())
		return m, loadDetailDataCmd(m.harnessFor(sess), sess)

	case "copy_id":
		// Best-effort clipboard copy; silently ignore errors.
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		if sess.ID != "" {
			copyToClipboard(sess.ID)
		}
		return m, nil

	case "copy_path":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		if sess.CWD != "" {
			copyToClipboard(sess.CWD)
		}
		return m, nil

	case "copy_branch":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		if branch := m.branchFor(sess); branch != "" {
			copyToClipboard(branch)
		}
		return m, nil

	case "copy_resume":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		cmd, err := resumeCommandFor(m.harnessFor(sess), sess, false)
		if err != nil {
			m.statusbar.SetError(err.Error())
			return m, nil
		}
		if sess.CWD != "" {
			cmd = fmt.Sprintf("cd %s && %s", tmux.ShellQuote(sess.CWD), cmd)
		}
		copyToClipboard(cmd)
		return m, nil

	case "open_editor":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m.openInEditor(sess)

	case "open_folder":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		if err := openFolder(sess.CWD); err != nil {
			m.statusbar.SetError(err.Error())
		}
		return m, nil
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Session refresh: progressive two-phase approach
// ---------------------------------------------------------------------------

// buildActiveIDSet returns the set of non-empty session IDs in active, used to
// exclude currently-active sessions from the closed/history scans.
func buildActiveIDSet(active []*session.Session) map[string]bool {
	ids := make(map[string]bool, len(active))
	for _, s := range active {
		if s.ID != "" {
			ids[s.ID] = true
		}
	}
	return ids
}

// enrichActiveSummaries fills a missing Summary on each active session from its
// harness's history index (first prompt).
func enrichActiveSummaries(reg *harness.Registry, active []*session.Session) {
	byHarness := partitionByHarness(reg, active)
	for name, list := range byHarness {
		h, _ := reg.Get(name)
		hp, ok := harness.AsHistory(h)
		if !ok {
			continue
		}
		historyIndex, _ := hp.Summaries()
		for _, s := range list {
			if s.ID != "" {
				if display, ok := historyIndex[s.ID]; ok && s.Summary == "" {
					s.Summary = display
				}
			}
		}
	}
}

// scanClosedAndAllSessions returns the time-windowed and full closed-session
// lists in one pass per history-capable harness, excluding active IDs.
func scanClosedAndAllSessions(reg *harness.Registry, closedHours float64, active []*session.Session) (closed, all []*session.Session) {
	if reg == nil {
		return nil, nil
	}
	byHarness := partitionByHarness(reg, active)
	for _, h := range reg.All() {
		hp, ok := harness.AsHistory(h)
		if !ok {
			continue
		}
		c, a, _ := hp.ScanClosedAndAll(closedHours, buildActiveIDSet(byHarness[h.Info().Name]))
		closed = append(closed, c...)
		all = append(all, a...)
	}
	return closed, all
}

// refreshActiveCmd performs the fast active-session scan only (no history).
func (m Model) refreshActiveCmd() tea.Cmd {
	detector := m.detector
	reg := m.registry

	return func() tea.Msg {
		active, err := detector.Detect()
		if err != nil {
			return activeSessionsMsg{err: err}
		}
		enrichActiveSummaries(reg, active)
		return activeSessionsMsg{active: active}
	}
}

// refreshHistoryCmd performs the slower closed/history scan.
func (m Model) refreshHistoryCmd() tea.Cmd {
	reg := m.registry
	closedHours := m.closedSessionHours
	activeSessions := m.activeSessions

	return func() tea.Msg {
		closed, all := scanClosedAndAllSessions(reg, closedHours, activeSessions)
		return historySessionsMsg{
			closed: closed,
			all:    all,
		}
	}
}

// refreshSessionsCmd performs the full combined refresh (used by periodic ticker).
func (m Model) refreshSessionsCmd() tea.Cmd {
	detector := m.detector
	reg := m.registry
	closedHours := m.closedSessionHours

	return func() tea.Msg {
		active, err := detector.Detect()
		if err != nil {
			return sessionRefreshMsg{err: err}
		}

		closed, all := scanClosedAndAllSessions(reg, closedHours, active)
		enrichActiveSummaries(reg, active)

		return sessionRefreshMsg{
			active: active,
			closed: closed,
			all:    all,
		}
	}
}

func (m *Model) applySessionRefresh(msg sessionRefreshMsg) tea.Cmd {
	prevSel := m.selectionIdentity()

	msg.active = m.dropKilling(msg.active)
	// Store active sessions for progressive merge.
	msg.active = m.mergePendingPlaceholder(msg.active)
	m.activeSessions = msg.active

	// Combine active and closed for the sidebar.
	combined := make([]*session.Session, 0, len(msg.active)+len(msg.closed))
	combined = append(combined, msg.active...)
	combined = append(combined, msg.closed...)

	// Apply user-defined display name aliases.
	m.applyAliases(combined)

	m.sessions = combined

	// If kill confirmation is pending and the target session is gone, cancel it.
	if m.confirmKill && m.confirmSession != nil {
		found := false
		for _, s := range combined {
			if s.TmuxTarget == m.confirmSession.TmuxTarget {
				found = true
				break
			}
		}
		if !found {
			m.confirmKill = false
			m.statusbar.SetConfirmKill(false)
			m.confirmSession = nil
			m.sidebar.ConfirmKillTarget = ""
		}
	}

	// Don't overwrite the sidebar during search -- the search model controls
	// what the sidebar displays via filtered results.
	if m.mode != ModeSearch {
		m.sidebar.SetSessions(combined)
	}

	// All sessions (active + all closed) for search.
	allCombined := make([]*session.Session, 0, len(msg.active)+len(msg.all))
	allCombined = append(allCombined, msg.active...)
	allCombined = append(allCombined, msg.all...)
	// Apply aliases to all sessions too (msg.all may have sessions not in combined).
	m.applyAliases(allCombined)
	m.allSessions = allCombined
	m.search.SetSessions(allCombined)

	// During search, push the re-filtered results to the sidebar so it
	// reflects any sessions that appeared/disappeared during the refresh.
	if m.mode == ModeSearch {
		m.sidebar.SetSessions(m.search.Results())
	}

	return tea.Batch(m.syncSelectionAfterRebuild(prevSel), m.saveLiveStateCmd(msg.active))
}

// selectionIdentity names the current sidebar selection across list rebuilds.
func (m Model) selectionIdentity() string {
	if sel := m.sidebar.SelectedSession(); sel != nil {
		return "s|" + clickID(sel)
	}
	return "g|" + m.sidebar.SelectedGroupName()
}

// syncSelectionAfterRebuild re-syncs the panes after SetSessions: a full sync if
// the cursor fell onto another row, else a header refresh (unless a settle is due).
func (m *Model) syncSelectionAfterRebuild(prev string) tea.Cmd {
	if m.selectionPending {
		return nil
	}
	if m.selectionIdentity() != prev {
		return m.afterSelectionChange()
	}
	if sel := m.sidebar.SelectedSession(); sel != nil {
		m.selectPreviewSession(sel)
	}
	return nil
}

// clearSeenAttentionCmd drops the previewed session's hook marker (it is seen) and
// "waiting" markers made stale by Claude's own status.
func (m *Model) clearSeenAttentionCmd() tea.Cmd {
	sel := m.sidebar.SelectedSession()
	var panes []string
	for _, s := range m.activeSessions {
		if s.Attention == "" || s.TmuxPaneID == "" {
			continue
		}
		seen := sel != nil && sel.TmuxPaneID == s.TmuxPaneID && s.TmuxPaneID != m.unreadHoldPane
		stale := s.Attention == attention.Waiting && s.NativeStatus && s.Status != session.StatusWaiting
		if seen || stale {
			panes = append(panes, s.TmuxPaneID)
		}
	}
	if len(panes) == 0 {
		return nil
	}
	cfg := m.cfg.Notifications
	return func() tea.Msg {
		for _, p := range panes {
			attention.Clear(cfg, p)
		}
		return nil
	}
}

// selectPreviewSession points the preview at s; on a pane change it fits the new
// window to the viewport and restores the previous window's size.
func (m *Model) selectPreviewSession(s *session.Session) {
	target := ""
	if s != nil {
		target = s.TmuxTarget
	}

	if target != m.previewTarget {
		m.unreadHoldPane = ""
		m.restorePreviewedPane()

		m.previewTarget = target
		m.preview.ClearSelection()
		m.preview.SetContent("")
		m.preview.ResetScroll()

		// For subprocesses, show a styled informational message.
		if s != nil && s.Subprocess {
			parent := s.SubprocessParent
			if parent == "" {
				parent = "another program"
			}
			label := "The agent"
			if h := m.harnessFor(s); h != nil {
				label = h.Info().Label
			}
			dim := lipgloss.NewStyle().Foreground(styles.ColorGray)
			accent := lipgloss.NewStyle().Foreground(styles.ColorBlue)
			hint := lipgloss.NewStyle().Foreground(styles.ColorAmber)
			m.preview.SetContent(fmt.Sprintf(
				"\n\n%s\n\n%s\n%s\n\n%s",
				dim.Italic(true).Render("  Preview unavailable"),
				dim.Render("  "+label+" is running inside "+accent.Render(parent)+dim.Render(".")),
				dim.Render("  The tmux pane belongs to "+accent.Render(parent)+dim.Render(", not "+label+".")),
				hint.Render("  Press "+accent.Render(m.keys.Jump)+" to jump to the pane"),
			))
		}

		// Resize the new pane's window to the preview viewport size.
		// Skip subprocesses (pane belongs to parent app) and naviClaude's own window.
		naviWinTarget := m.currentTmuxWin
		if s != nil && s.TmuxTarget != "" && !s.Subprocess && tmux.WindowTarget(s.TmuxTarget) != naviWinTarget {
			winTarget := tmux.WindowTarget(s.TmuxTarget)
			// Save the original size so we can restore it when navigating away.
			if w, h, err := m.tmuxClient.WindowSize(winTarget); err == nil {
				m.origWinW, m.origWinH = w, h
			} else {
				m.origWinW, m.origWinH = m.width, m.height
			}
			m.resizedWinTarget = winTarget
			m.tmuxClient.SaveWindowSize(winTarget)
			paneW, paneH := m.previewPaneDimensions()
			m.tmuxClient.ResizeWindow(winTarget, paneW, paneH)
		}
	}

	if s != nil {
		m.preview.SetSession(s)
	}
}

// restorePreviewedPane restores the currently previewed pane's window to its
// original size. Called on quit and when leaving naviClaude.
func (m *Model) restorePreviewedPane() {
	if m.resizedWinTarget != "" && m.origWinW > 0 {
		m.tmuxClient.RestoreWindowSize(m.resizedWinTarget, m.origWinW, m.origWinH)
		m.resizedWinTarget = ""
		m.origWinW, m.origWinH = 0, 0
	}
}

// ---------------------------------------------------------------------------
// Preview capture (async command)
// ---------------------------------------------------------------------------

func (m Model) capturePreviewCmd() tea.Cmd {
	sess := m.sidebar.SelectedSession()
	if sess == nil || m.selectionPending {
		// Mid-navigation the preview still shows the previous session; the
		// settle captures the new one.
		return nil
	}

	if sess.Status == session.StatusClosed {
		// No pane to capture for a closed session. Don't overwrite
		// any conversation history that was loaded via loadClosedPreviewCmd.
		return nil
	}

	if sess.Subprocess {
		return nil
	}

	target := sess.TmuxTarget
	sessionID := sess.ID
	statusSess := *sess
	h := m.harnessFor(sess)
	captureEngine := m.captureEngine
	tc := m.tmuxClient
	// Compute the width here, where dimensions are stable, since a shared field
	// would race the capture goroutine; -2 is the preview's padding.
	previewWidth := m.width - m.sidebarWidth()
	maxWidth := previewWidth - 2
	// Outside passthrough the pane isn't receiving input, so a cursor would mislead.
	showCursor := m.mode == ModePassthrough

	return func() tea.Msg {
		// Query the mouse mode alongside the capture rather than after it.
		fwd := make(chan bool, 1)
		go func() {
			ok, _ := tc.WheelForwardable(target)
			fwd <- ok
		}()
		content, err := captureEngine.Capture(target, showCursor, maxWidth)
		if err != nil {
			return previewCaptureMsg{err: err, target: target, sessionID: sessionID}
		}

		// The harness's native status is authoritative when available; content
		// classification is only the fallback, so a guess never overrides it.
		status := session.StatusActive
		if h != nil {
			status = harness.PreviewStatus(h, &statusSess, content)
		}

		return previewCaptureMsg{
			content:       content,
			target:        target,
			sessionID:     sessionID,
			status:        status,
			forwardsWheel: <-fwd,
		}
	}
}

// loadClosedPreviewCmd loads conversation history from a closed session's .jsonl
// and formats it for display in the preview pane with colored styling.
func (m Model) loadClosedPreviewCmd(sess *session.Session) tea.Cmd {
	h := m.harnessFor(sess)
	return func() tea.Msg {
		cp, ok := harness.AsConversation(h)
		if h == nil || !ok {
			return closedPreviewMsg{content: "  No conversation history found."}
		}
		assistantLabel := h.Info().AssistantLabel
		entries, err := cp.LoadConversation(sess, 100)
		if err != nil {
			return closedPreviewMsg{err: err}
		}
		if len(entries) == 0 {
			return closedPreviewMsg{content: "  No conversation history found."}
		}

		sep := styles.ConversationSeparator.Render(strings.Repeat("\u2500", 40))

		var b strings.Builder
		for i, e := range entries {
			if i > 0 {
				b.WriteString("\n  " + sep + "\n")
			}
			if e.Role == "user" {
				b.WriteString("\n  " + styles.ConversationUserLabel.Render("You") + "\n")
				lines := strings.Split(e.Text, "\n")
				for _, line := range lines {
					b.WriteString("  " + styles.ConversationUserText.Render("  "+line) + "\n")
				}
			} else {
				b.WriteString("\n  " + styles.ConversationAssistantLabel.Render(assistantLabel) + "\n")
				lines := strings.Split(e.Text, "\n")
				for _, line := range lines {
					b.WriteString("  " + styles.ConversationAssistantText.Render("  "+line) + "\n")
				}
			}
		}
		return closedPreviewMsg{content: b.String()}
	}
}

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

func (m *Model) resizeComponents() {
	sidebarWidth := m.sidebarWidth()
	rightWidth := m.rightSidebarWidth()
	previewWidth := m.width - sidebarWidth - rightWidth

	// Measure chrome heights dynamically to stay in sync with View().
	titleHeight := lipgloss.Height(m.renderTitleBar())
	m.statusbar.SetSize(m.width)
	statusHeight := lipgloss.Height(m.statusbar.View())
	contentHeight := m.height - titleHeight - statusHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	m.sidebar.SetSize(sidebarWidth-1, contentHeight)
	m.preview.SetSize(previewWidth, contentHeight)
	if rightWidth > 0 {
		m.rightSidebar.SetSize(rightWidth-1, contentHeight)
	}
	m.search.SetSize(sidebarWidth-1, contentHeight)
	m.help.SetSize(m.width, m.height)
	m.detail.SetSize(m.width, m.height)
	m.statsModel.SetSize(m.width, m.height)
	m.themePicker.SetSize(m.width, m.height)
	m.dirPicker.SetSize(m.width, m.height)
	m.resumePicker.SetSize(m.width, m.height)
	m.contextMenu.SetTermSize(m.width, m.height)
}

// resizePreviewedPane fits the previewed tmux window to the preview viewport;
// call it whenever the preview area changes size.
func (m *Model) resizePreviewedPane() {
	if m.resizedWinTarget != "" {
		paneW, paneH := m.previewPaneDimensions()
		m.tmuxClient.ResizeWindow(m.resizedWinTarget, paneW, paneH)
	}
}

// previewPaneDimensions returns the size managed tmux panes are resized to so
// their content fits the preview viewport exactly.
func (m Model) previewPaneDimensions() (int, int) {
	sidebarWidth := m.sidebarWidth()
	previewWidth := m.width - sidebarWidth - m.rightSidebarWidth()
	paneW := previewWidth - 2 // preview left/right padding
	if paneW < 1 {
		paneW = 1
	}

	titleHeight := lipgloss.Height(m.renderTitleBar())
	statusHeight := lipgloss.Height(m.statusbar.View())
	contentHeight := m.height - titleHeight - statusHeight
	paneH := contentHeight - 2 // preview header + border-bottom
	if paneH < 1 {
		paneH = 1
	}
	return paneW, paneH
}

// renderTitleBar draws the one-row title: app name and per-status session
// counts on the left, the current tmux session on the right.
func (m Model) renderTitleBar() string {
	name := styles.TitleBarName.Render(" naviClaude")

	tmuxSess := m.currentTmuxSession
	if tmuxSess == "" {
		tmuxSess = "unknown"
	}
	right := styles.TitleBarDim.Render(tmuxSess + " ")

	const summaryGap = "  "
	leftLine := name
	budget := m.width - lipgloss.Width(name) - len(summaryGap) - lipgloss.Width(right) - 1
	if summary := renderStatusSummary(countStatuses(m.sessions, m.background), budget); summary != "" {
		leftLine += summaryGap + summary
	}

	gap := m.width - lipgloss.Width(leftLine) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	bar := ansi.Truncate(leftLine+strings.Repeat(" ", gap)+right, m.width, "")
	// No underline: the pane headers right below already separate the title.
	return styles.TitleBar.BorderBottom(false).Width(m.width).Render(bar)
}

func (m Model) sidebarWidth() int {
	w := m.width * m.sidebarWidthPct / 100
	if w < 20 {
		w = 20
	}
	if w > m.width-20 {
		w = m.width - 20
	}
	return w
}

// rightSidebarWidth returns the changed-files panel width, or 0 when collapsed,
// capped so the left sidebar and preview always keep room.
func (m Model) rightSidebarWidth() int {
	if !m.rightPanelOpen {
		return 0
	}
	w := m.width * m.sidebarWidthPct / 100
	if w < 24 {
		w = 24
	}
	// Keep the left sidebar (>=20) plus a minimum preview (>=20) usable.
	if max := m.width - m.sidebarWidth() - 20; w > max {
		w = max
	}
	if w < 1 {
		w = 0
	}
	return w
}

// ---------------------------------------------------------------------------
// Session selection helper
// ---------------------------------------------------------------------------

// selectSessionInSidebar moves the sidebar cursor and the preview to target.
func (m *Model) selectSessionInSidebar(target *session.Session) {
	if target == nil {
		return
	}
	m.sidebar.SelectByID(target.Key())
	m.preview.SetSession(target)
}

// reloadMetricsForSelection clears and reloads metrics when sel differs from
// the session they belong to, returning the load command or nil.
func (m *Model) reloadMetricsForSelection(sel *session.Session) tea.Cmd {
	if sel == nil || sel.ID == m.metricsSessionID {
		return nil
	}
	m.currentMetrics = nil
	m.metricsSessionID = sel.ID
	m.sidebar.SetMetrics(sel.ID, nil)
	m.preview.SetMetrics(sel.ID, nil)
	if sel.ID == "" {
		return nil // placeholder: nothing to load until the real session is detected
	}
	return tea.Batch(loadMetricsCmd(m.harnessFor(sel), sel), loadSidebarDetailCmd(m.harnessFor(sel), sel))
}

// reloadChangedFilesForSelection reloads the changed files for a new selection;
// they feed the sidebar repo line too, so this runs with the panel closed.
func (m *Model) reloadChangedFilesForSelection(sel *session.Session) tea.Cmd {
	if sel == nil || sel.ID == "" || sel.ID == m.changedFilesSessionID {
		return nil
	}
	m.changedFilesSessionID = sel.ID
	m.changedFilesMtime = time.Time{} // pending load; suppress live refresh until it lands
	m.rightSidebar.SetFiles(nil, sel.CWD)
	return loadChangedFilesCmd(m.harnessFor(sel), sel)
}

// maybeRefreshChangedFilesCmd reloads the selected session's changed files once
// its transcript advanced; the stat is per-tick cheap, the scan + git diff are not.
func (m *Model) maybeRefreshChangedFilesCmd() tea.Cmd {
	sel := m.sidebar.SelectedSession()
	if sel == nil || sel.ID == "" || m.selectionPending {
		return nil
	}
	if sel.ID != m.changedFilesSessionID {
		// Selected without a sync (startup, restore): load it now.
		return m.reloadChangedFilesForSelection(sel)
	}
	if m.changedFilesMtime.IsZero() {
		return nil // initial (or post-switch) load still in flight
	}
	cf, ok := harness.AsChangedFiles(m.harnessFor(sel))
	if !ok || !cf.ChangeStamp(sel).After(m.changedFilesMtime) {
		return nil
	}
	return loadChangedFilesCmd(m.harnessFor(sel), sel)
}

// setFocus moves keyboard focus to target, applying passthrough enter/leave side
// effects; entering the changed-files pane returns its reload command.
func (m *Model) setFocus(target Mode) tea.Cmd {
	if m.mode == ModePassthrough && target != ModePassthrough {
		m.preview.SetPassthrough(false)
		m.pendingNewTarget = ""
	}
	if target == ModePassthrough && m.mode != ModePassthrough {
		m.unreadHoldPane = "" // typing into the pane counts as reading it
		m.preview.SetPassthrough(true)
	}
	m.mode = target
	m.statusbar.SetMode(target.String())

	if target == ModeChangedFiles {
		if sel := m.sidebar.SelectedSession(); sel != nil {
			return m.reloadChangedFilesForSelection(sel)
		}
	}
	return nil
}

// nextFocusMode returns the next pane in the left-to-right Tab ring, skipping the
// preview for non-live sessions and the changed-files panel when closed.
func (m Model) nextFocusMode(forward bool) Mode {
	sel := m.sidebar.SelectedSession()
	canPassthrough := sel != nil && sel.Status != session.StatusClosed && sel.TmuxTarget != ""

	ring := []Mode{ModeList}
	if canPassthrough {
		ring = append(ring, ModePassthrough)
	}
	if m.rightPanelOpen {
		ring = append(ring, ModeChangedFiles)
	}

	idx := 0
	for i, mode := range ring {
		if mode == m.mode {
			idx = i
			break
		}
	}
	n := len(ring)
	if forward {
		idx = (idx + 1) % n
	} else {
		idx = (idx - 1 + n) % n
	}
	return ring[idx]
}

// applyAliases overlays user display-name aliases onto list in place, so
// refresh, rename and search all show the same names.
func (m *Model) applyAliases(list []*session.Session) {
	aliases := m.aliasStore.All()
	if len(aliases) == 0 {
		return
	}
	for _, s := range list {
		if name, ok := aliases[s.Key()]; ok {
			s.DisplayName = name
		}
	}
}

// enterNewSessionPassthrough selects a new/resumed placeholder at the front and
// enters passthrough, expanding its group so a collapsed group can't hide it.
func (m *Model) enterNewSessionPassthrough(placeholder *session.Session) {
	m.sessions = append([]*session.Session{placeholder}, m.sessions...)
	m.sidebar.SetSessions(m.sessions)
	m.sidebar.ExpandGroup(placeholder.TmuxSession)
	m.sidebar.SelectByTarget(placeholder.TmuxTarget)
	m.preview.SetSession(placeholder)
	m.pendingNewTarget = placeholder.TmuxTarget
	m.mode = ModePassthrough
	m.preview.SetPassthrough(true)
	m.statusbar.SetMode(ModePassthrough.String())
}

// mergePendingPlaceholder clears pendingNewTarget once it appears in active;
// until then it carries the placeholder forward so it stays visible.
func (m *Model) mergePendingPlaceholder(active []*session.Session) []*session.Session {
	if m.pendingNewTarget == "" {
		return active
	}
	for _, s := range active {
		if s.TmuxTarget == m.pendingNewTarget {
			m.pendingNewTarget = ""
			return active
		}
	}
	for _, s := range m.sessions {
		if s.TmuxTarget == m.pendingNewTarget {
			// Prepend so the placeholder stays where creation put it instead of
			// jumping on the next refresh.
			return append([]*session.Session{s}, active...)
		}
	}
	return active
}

// dropKilling filters out sessions whose kill is still in flight.
func (m *Model) dropKilling(active []*session.Session) []*session.Session {
	if len(m.killing) == 0 {
		return active
	}
	kept := make([]*session.Session, 0, len(active))
	for _, s := range active {
		if !m.killing[s.TmuxTarget] {
			kept = append(kept, s)
		}
	}
	return kept
}

// removeSession removes a session by tmux target from all internal lists and
// rebuilds the sidebar immediately.
func (m *Model) removeSession(tmuxTarget string) {
	filter := func(list []*session.Session) []*session.Session {
		result := make([]*session.Session, 0, len(list))
		for _, s := range list {
			if s.TmuxTarget != tmuxTarget {
				result = append(result, s)
			}
		}
		return result
	}
	m.sessions = filter(m.sessions)
	m.activeSessions = filter(m.activeSessions)
	m.allSessions = filter(m.allSessions)
	m.sidebar.SetSessions(m.sessions)
	m.search.SetSessions(m.allSessions)
}

func (m *Model) updateSessionStatus(target string, status session.SessionStatus) {
	for _, s := range m.sessions {
		if s.TmuxTarget == target {
			s.Status = status
			break
		}
	}
}

// ---------------------------------------------------------------------------
// Ticker constructors
// ---------------------------------------------------------------------------

func (m Model) tickPreviewCmd() tea.Cmd {
	return tea.Tick(m.refreshInterval, func(t time.Time) tea.Msg {
		return tickPreviewMsg{}
	})
}

func tickSession() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return tickSessionMsg{}
	})
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// detectCurrentTmuxWindow returns the "session:window" target of the window
// that naviClaude is running in, computed once at startup.
func detectCurrentTmuxWindow() string {
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}:#{window_index}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// detectCurrentTmuxSession returns the name of the tmux session that
// naviClaude is running inside, or an empty string if it cannot be determined.
func detectCurrentTmuxSession() string {
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// copyToClipboard copies text to the system clipboard via pbcopy, xclip, then
// xsel, returning the last tool's error if none works.
func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if cmd.Run() == nil {
		return nil
	}
	cmd = exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = strings.NewReader(text)
	if cmd.Run() == nil {
		return nil
	}
	cmd = exec.Command("xsel", "--clipboard", "--input")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// maybeRestoreLastSession re-selects the session focused at last quit, once, if
// focus_last_session is on; it returns that session's metrics-load Cmd or nil.
func (m *Model) maybeRestoreLastSession() tea.Cmd {
	if m.restoredLastSession || !m.cfg.FocusLastSession || m.viewState == nil {
		return nil
	}
	id := m.viewState.Get().LastSessionID
	if id == "" {
		return nil
	}
	if !m.sidebar.SelectByID(id) {
		// Not present yet; a later scan (history) may still contain it.
		return nil
	}
	m.restoredLastSession = true
	sel := m.sidebar.SelectedSession()
	if sel == nil {
		return nil
	}
	m.selectPreviewSession(sel)
	return m.reloadMetricsForSelection(sel)
}

// persistViewState saves the focused session and manually toggled groups at quit;
// each field is applied on startup only if its config toggle is on.
func (m Model) persistViewState() {
	if m.viewState == nil {
		return
	}
	vs := session.ViewState{CollapsedGroups: m.sidebar.ToggledGroups()}
	if sel := m.sidebar.SelectedSession(); sel != nil {
		vs.LastSessionID = sel.Key()
	}
	_ = m.viewState.Save(vs)
}

// Ensure Model satisfies tea.Model at compile time.
var _ tea.Model = Model{}
