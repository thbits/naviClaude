package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thbits/naviClaude/internal/config"
	"github.com/thbits/naviClaude/internal/preview"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/stats"
	"github.com/thbits/naviClaude/internal/styles"
	"github.com/thbits/naviClaude/internal/tmux"
	"github.com/thbits/naviClaude/internal/ui"
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
	content   string
	target    string
	sessionID string
	status    session.SessionStatus
	err       error
}

// closedPreviewMsg carries formatted conversation history for a closed session.
type closedPreviewMsg struct {
	content string
	err     error
}

// newSessionMsg carries the result of creating a new Claude session.
type newSessionMsg struct {
	tmuxTarget  string
	tmuxSession string
	cwd         string
	err         error
}

// newTmuxSessionMsg carries the result of creating a new tmux session with Claude.
type newTmuxSessionMsg struct {
	tmuxTarget  string
	tmuxSession string
	cwd         string
	err         error
}

// errMsg is a transient error notification.
type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

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
	tmuxClient     *tmux.Client
	detector       *session.Detector
	historyScanner *session.HistoryScanner
	manager        *session.Manager
	captureEngine  *preview.CaptureEngine
	passthrough    *preview.Passthrough
	statusDetector *preview.StatusDetector

	// Application state
	mode                 Mode
	width, height        int
	sessions             []*session.Session // active + recently-closed (for sidebar)
	allSessions          []*session.Session // all sessions (for search)
	activeSessions       []*session.Session // just the active sessions (for progressive merge)
	err                  error              // last error, shown briefly
	confirmKill          bool               // waiting for kill confirmation
	confirmSession       *session.Session   // session pending kill confirmation
	pendingNewTarget     string             // tmux target of a just-created session; kept until detected
	pendingNewTmuxCWD    string             // CWD for the tmux session being named
	pendingDirAction     dirAction          // which flow opened the directory picker
	pendingDirTmux       string             // target tmux session for the new-Claude (n) flow
	renameSessionID      string             // session ID being renamed
	pendingResumeSession *session.Session   // closed session awaiting a resume-target choice
	previewTarget        string             // tmux target currently shown in the preview panel
	resizedWinTarget     string             // window target we resized for preview
	origWinW             int                // width to restore the previewed window to when navigating away

	// Metrics for currently selected session
	currentMetrics   *session.SessionMetrics
	metricsSessionID string // session ID the metrics belong to

	// Session aliases (user-defined display names)
	aliasStore *session.AliasStore

	// Stats cache
	statsCache *stats.Cache

	// Animation
	breathingFrame int
	spinner        spinner.Model

	// Configuration
	cfg                config.Config
	keys               KeyMap
	sidebarWidthPct    int
	closedSessionHours float64
	processNames       []string
	currentTmuxSession string        // the tmux session naviClaude is running in
	currentTmuxWin     string        // "session:window" target of naviClaude's window
	isPopup            bool          // true when running inside tmux display-popup
	refreshInterval    time.Duration // preview capture tick interval
}

// New creates a fully-wired Model ready to be passed to tea.NewProgram.
func New(version string) Model {
	cfg, _ := config.Load("")
	styles.ApplyTheme(styles.Named(cfg.Theme))
	keys := KeyMapFromConfig(cfg.Keys)

	refreshInterval, err := time.ParseDuration(cfg.RefreshInterval)
	if err != nil || refreshInterval < 50*time.Millisecond {
		refreshInterval = 200 * time.Millisecond
	}

	tc := tmux.New()
	hs, _ := session.NewHistoryScanner("")

	m := Model{
		// UI
		sidebar:      ui.NewSidebar(30, 24),
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
		tmuxClient:     tc,
		detector:       session.NewDetector(tc, cfg.ProcessNames, cfg.ActiveWindowSecs, cfg.CPUActiveThreshold),
		historyScanner: hs,
		manager:        session.NewManager(tc),
		captureEngine:  preview.NewCaptureEngine(tc),
		passthrough:    preview.NewPassthrough(tc),
		statusDetector: preview.NewStatusDetector(),

		// Session aliases
		aliasStore: session.NewAliasStore(""),

		// Stats cache
		statsCache: stats.NewCache(),

		// Configuration
		cfg:                cfg,
		keys:               keys,
		mode:               ModeList,
		sidebarWidthPct:    cfg.SidebarWidth,
		closedSessionHours: cfg.ClosedSessionHours,
		processNames:       cfg.ProcessNames,
		currentTmuxSession: detectCurrentTmuxSession(),
		currentTmuxWin:     detectCurrentTmuxWindow(),
		isPopup:            os.Getenv("TMUX_POPUP") != "",
		refreshInterval:    refreshInterval,
	}

	// Initialize loading spinner.
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(styles.ColorBlue)
	m.spinner = s

	// Wire auto-collapse threshold and sort orders.
	m.sidebar.SetCollapseAfterHours(cfg.CollapseAfterHours)
	m.sidebar.SetGroupSortOrder(cfg.GroupSortOrder)
	m.sidebar.SetSessionSortOrder(cfg.SessionSortOrder)

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

	return m
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
	return tea.Batch(
		m.tickPreviewCmd(),
		tickSession(),
		tickResource(),
		m.refreshActiveCmd(),
		breathingTickCmd(),
		m.spinner.Tick,
	)
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
		// Re-resize the currently previewed pane's width at the new dimensions.
		if m.resizedWinTarget != "" {
			m.origWinW = msg.Width
			paneW, _ := m.previewPaneDimensions()
			m.tmuxClient.ResizeWindow(m.resizedWinTarget, paneW, 0)
		}
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

	case detailDataMsg:
		// Only apply if the detail popup is still showing the same session the
		// read was fired for; a slow read must not repaint after the user
		// reopened the popup on a different session (mirrors metricsMsg).
		if sel := m.sidebar.SelectedSession(); sel != nil && sel.ID == msg.sessionID {
			m.detail.SetData(msg.messageCount, msg.startTime)
		}
		return m, nil

	case statsComputeMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("stats: %w", msg.err)
			m.statsModel.Hide()
			m.mode = ModeList
			m.statusbar.SetMode(ModeList.String())
		} else {
			m.statsModel.SetStats(msg.stats)
		}
		return m, nil

	case metricsMsg:
		// Only apply if metrics are still for the currently selected session.
		if sel := m.sidebar.SelectedSession(); sel != nil && sel.ID == msg.sessionID {
			m.currentMetrics = msg.metrics
			m.metricsSessionID = msg.sessionID
			m.sidebar.SetMetrics(msg.metrics)
			m.preview.SetMetrics(msg.metrics)
		}
		return m, nil

	// -- Async results (progressive: active first, then history) -------------
	case activeSessionsMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			active := m.mergePendingPlaceholder(msg.active)
			m.activeSessions = active
			// Don't push active-only to the sidebar if we already have closed
			// sessions -- it would temporarily remove them and destabilize the
			// cursor. The combined list is rebuilt once history arrives.
			if len(m.sessions) == 0 {
				m.sessions = active
				if m.mode != ModeSearch {
					m.sidebar.SetSessions(active)
				}
			}
			if sel := m.sidebar.SelectedSession(); sel != nil {
				m.preview.SetSession(sel)
				// Fire metrics load for the initially selected session.
				if sel.ID != "" && sel.ID != m.metricsSessionID {
					m.metricsSessionID = sel.ID
					return m, tea.Batch(m.refreshHistoryCmd(), loadMetricsCmd(sel))
				}
			}
		}
		// Fire the slower history scan.
		return m, m.refreshHistoryCmd()

	case historySessionsMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			// Merge active + closed for the sidebar.
			combined := make([]*session.Session, 0, len(m.activeSessions)+len(msg.closed))
			combined = append(combined, m.activeSessions...)
			combined = append(combined, msg.closed...)
			m.sessions = combined
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

			if sel := m.sidebar.SelectedSession(); sel != nil {
				m.preview.SetSession(sel)
			}
		}
		return m, nil

	// -- Full combined refresh (from periodic ticker) -----------------------
	case sessionRefreshMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.applySessionRefresh(msg)
		}
		return m, nil

	case previewCaptureMsg:
		// Discard stale results: if the user navigated away while the capture
		// was in flight, the result belongs to a different session. Require BOTH
		// the tmux target AND the session ID to match the current selection -- a
		// reused pane target alone is not enough, since a different session can
		// reuse the same target after the old one closed.
		if sel := m.sidebar.SelectedSession(); sel == nil || sel.TmuxTarget != msg.target || sel.ID != msg.sessionID {
			return m, nil
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
			// Only apply Waiting status from the preview path. Active/Idle are
			// authoritative from the session detector; applying Active from
			// captures causes flickering due to spinners and cursor blink. Apply
			// Waiting regardless of content emptiness so a native "waiting" status
			// is surfaced immediately even before the pane renders the prompt.
			if msg.status == session.StatusWaiting {
				m.updateSessionStatus(msg.target, msg.status)
			}
		}
		return m, nil

	case newSessionMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("new session: %w", msg.err)
			return m, nil
		}
		// Create a placeholder session and enter passthrough immediately.
		placeholder := &session.Session{
			TmuxSession: msg.tmuxSession,
			TmuxTarget:  msg.tmuxTarget,
			CWD:         msg.cwd,
			ProjectName: "claude",
			Status:      session.StatusActive,
		}
		// Prepend to session list so it appears right away.
		m.sessions = append([]*session.Session{placeholder}, m.sessions...)
		m.sidebar.SetSessions(m.sessions)
		m.sidebar.SelectByID("") // won't match, so find by target below
		// Select by target since ID is unknown yet.
		for i, item := range m.sidebar.FlatItems() {
			if item.Session != nil && item.Session.TmuxTarget == msg.tmuxTarget {
				m.sidebar.SetCursor(i)
				break
			}
		}
		m.preview.SetSession(placeholder)
		m.pendingNewTarget = msg.tmuxTarget
		m.mode = ModePassthrough
		m.preview.SetPassthrough(true)
		m.statusbar.SetMode(ModePassthrough.String())
		// Fire a refresh so the real session (with ID etc.) replaces the placeholder.
		return m, m.refreshSessionsCmd()

	case newTmuxSessionMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("new tmux session: %w", msg.err)
			return m, nil
		}
		// Create a placeholder session and select it in the sidebar.
		placeholder := &session.Session{
			TmuxSession:  msg.tmuxSession,
			TmuxTarget:   msg.tmuxTarget,
			CWD:          msg.cwd,
			ProjectName:  filepath.Base(msg.cwd),
			Status:       session.StatusActive,
			LastActivity: time.Now(), // prevent auto-collapse of the new group
		}
		if placeholder.ProjectName == "" || placeholder.ProjectName == "." {
			placeholder.ProjectName = "claude"
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
		return m, m.refreshSessionsCmd()

	case closedPreviewMsg:
		if msg.err != nil {
			m.preview.SetContent(fmt.Sprintf("  Could not load session: %s", msg.err))
		} else {
			m.preview.SetContent(msg.content)
		}
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	// -- Key input -----------------------------------------------------------
	case tea.KeyMsg:
		return m.handleKey(msg)

	// -- Mouse input ---------------------------------------------------------
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}

	// Forward any message not handled above to the directory picker while it is
	// open. The picker's async candidate-load result (dirCandidatesMsg) is an
	// unexported ui-package type, so it can't be matched in the switch above;
	// routing unhandled messages to the visible picker delivers it to
	// dirPicker.Update, which applies the loaded candidates.
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
	previewWidth := m.width - sidebarWidth

	// Render chrome first so we can measure their actual heights.
	titleBar := m.renderTitleBar()
	statusView := m.statusbar.View()
	contentHeight := m.height - lipgloss.Height(titleBar) - lipgloss.Height(statusView)
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Pass spinner view to sidebar for the loading/empty state.
	m.sidebar.SetSpinnerView(m.spinner.View())

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

	// Apply the SidebarPanel style with right border as the separator.
	// In passthrough mode the border is blue; otherwise default border color.
	sidebarStyle := styles.SidebarPanel
	if m.mode == ModePassthrough {
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

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, previewView)

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
		// The context menu positions itself via MarginLeft/MarginTop; overlayString
		// composites it onto the screen at that position. (A prior lipgloss.Place
		// call here was dead -- its result was immediately overwritten by
		// overlayString -- so it has been removed.)
		menuView := m.contextMenu.View()
		screen = overlayString(screen, menuView, m.width, m.height)
	}

	return screen
}

// ---------------------------------------------------------------------------
// Key handling per mode
// ---------------------------------------------------------------------------

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.statusbar.ClearError()
	key := msg.String()

	// Global quit — but in passthrough mode Ctrl+C is forwarded to the pane.
	if key == KeyCtrlC && m.mode != ModePassthrough {
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
	}
	return m, nil
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case m.keys.Quit:
		m.restorePreviewedPane()
		return m, tea.Quit

	case m.keys.Help:
		m.help.Toggle()
		if m.help.IsVisible() {
			m.mode = ModeHelp
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

	case KeyEnter, KeyTab:
		sess := m.sidebar.SelectedSession()
		if sess == nil {
			// Cursor is on a group header; delegate to sidebar for collapse/expand.
			var cmd tea.Cmd
			m.sidebar, cmd = m.sidebar.Update(msg)
			return m, cmd
		}
		if sess.Status == session.StatusClosed {
			if key == KeyEnter {
				// Enter resumes a closed session: open the target picker to
				// choose which tmux session the resume opens in.
				return m.openResumePicker(sess)
			}
			// Tab still shows the conversation history in the preview pane.
			m.selectPreviewSession(sess)
			return m, m.loadClosedPreviewCmd(sess)
		}
		// Enter passthrough mode for active sessions.
		m.mode = ModePassthrough
		m.preview.SetPassthrough(true)
		m.statusbar.SetMode(ModePassthrough.String())
		return m, nil

	case m.keys.Jump:
		sess := m.sidebar.SelectedSession()
		if sess != nil && sess.Status == session.StatusClosed {
			// Resume closed session in a new tmux window.
			return m.resumeSession(sess)
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
		return m, loadDetailDataCmd(sess)

	case m.keys.Stats:
		m.statsModel.Show()
		m.mode = ModeStats
		m.statusbar.SetMode(ModeStats.String())
		return m, m.computeStatsCmd()

	case KeyThemePicker:
		m.themePicker.Show(m.cfg.Theme)
		m.mode = ModeThemePicker
		m.statusbar.SetMode(ModeThemePicker.String())
		return m, nil

	case "ctrl+u":
		m.preview.ScrollUp(m.preview.HalfViewHeight())
		return m, nil

	case "ctrl+d":
		m.preview.ScrollDown(m.preview.HalfViewHeight())
		return m, nil

	default:
		// Navigation keys: delegate to sidebar.
		var cmd tea.Cmd
		var cmds []tea.Cmd
		m.sidebar, cmd = m.sidebar.Update(msg)
		cmds = append(cmds, cmd)
		// After navigation, update preview session header.
		if sel := m.sidebar.SelectedSession(); sel != nil {
			m.selectPreviewSession(sel)
			// Fire metrics load if selection changed.
			if cmd := m.reloadMetricsForSelection(sel); cmd != nil {
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
		}
		return m, tea.Batch(cmds...)
	}
}

func (m Model) handlePassthroughKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case KeyExitPassthrough, KeyExitPassthrough2, KeyExitPassthrough3:
		m.mode = ModeList
		m.preview.SetPassthrough(false)
		m.pendingNewTarget = "" // stop forcing cursor to new session
		m.statusbar.SetMode(ModeList.String())
		return m, nil

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
		m.mode = ModePassthrough
		m.preview.SetPassthrough(true)
		m.statusbar.SetMode(ModePassthrough.String())
		return m, nil

	case "up", "ctrl+p", "down", "ctrl+n":
		// Navigate the sidebar (filtered results).
		var cmd tea.Cmd
		var cmds []tea.Cmd
		m.sidebar, cmd = m.sidebar.Update(msg)
		cmds = append(cmds, cmd)
		if sel := m.sidebar.SelectedSession(); sel != nil {
			m.selectPreviewSession(sel)
			// Reload metrics on selection change so they don't go stale while
			// navigating search results (mirrors list-mode navigation).
			if mc := m.reloadMetricsForSelection(sel); mc != nil {
				cmds = append(cmds, mc)
			}
			if sel.Status == session.StatusClosed {
				cmds = append(cmds, m.loadClosedPreviewCmd(sel))
			}
		}
		return m, tea.Batch(cmds...)

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
	m.renameSessionID = sess.ID
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
		action := m.contextMenu.SelectedAction()
		target := m.contextMenu.Session()
		m.contextMenu.Hide()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m.executeContextAction(action, target)

	case KeyMenuCancel:
		m.contextMenu.Hide()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
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
// Mouse handling
// ---------------------------------------------------------------------------

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	sidebarWidth := m.sidebarWidth()

	switch msg.Type {
	case tea.MouseLeft:
		if msg.X < sidebarWidth {
			// Click in sidebar area. Delegate to sidebar for item selection.
			// Simple approximation: compute which flat item was clicked.
			var cmd tea.Cmd
			m.sidebar, cmd = m.sidebar.Update(msg)
			if sel := m.sidebar.SelectedSession(); sel != nil {
				m.selectPreviewSession(sel)
			}
			return m, cmd
		}
		// Click in preview area: enter passthrough.
		sess := m.sidebar.SelectedSession()
		if sess != nil && sess.Status != session.StatusClosed {
			m.mode = ModePassthrough
			m.preview.SetPassthrough(true)
			m.statusbar.SetMode(ModePassthrough.String())
		}
		return m, nil

	case tea.MouseRight:
		if msg.X < sidebarWidth {
			sess := m.sidebar.SelectedSession()
			if sess != nil {
				m.contextMenu.Show(msg.X, msg.Y, sess)
				m.mode = ModeContextMenu
				m.statusbar.SetMode(ModeContextMenu.String())
			}
		}
		return m, nil

	case tea.MouseWheelUp, tea.MouseWheelDown:
		if msg.X < sidebarWidth {
			var cmd tea.Cmd
			m.sidebar, cmd = m.sidebar.Update(msg)
			return m, cmd
		}
		// Scroll in preview area.
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

func (m Model) jumpToPane() (tea.Model, tea.Cmd) {
	sess := m.sidebar.SelectedSession()
	if sess == nil || sess.TmuxTarget == "" {
		return m, nil
	}
	m.restorePreviewedPane()
	// Switch tmux client to the session's pane.
	_ = m.tmuxClient.SwitchClient(sess.TmuxTarget)
	_ = m.tmuxClient.SelectPane(sess.TmuxTarget)
	return m, tea.Quit
}

func (m Model) killSelected() (tea.Model, tea.Cmd) {
	sess := m.sidebar.SelectedSession()
	if sess == nil || sess.Status == session.StatusClosed {
		return m, nil
	}
	// Show inline kill confirmation on the sidebar item.
	m.confirmKill = true
	m.confirmSession = sess
	m.sidebar.ConfirmKillTarget = sess.TmuxTarget
	return m, nil
}

func (m Model) handleKillConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "y", "Y":
		var cmds []tea.Cmd
		if m.confirmSession != nil {
			// Run the actual kill in a Cmd so the tmux exec doesn't block the
			// event loop. Keep the optimistic removeSession so feedback is
			// instant regardless of the exec's latency.
			cmds = append(cmds, m.killSessionCmd(m.confirmSession))
			m.removeSession(m.confirmSession.TmuxTarget)
		}
		m.confirmKill = false
		m.confirmSession = nil
		m.sidebar.ConfirmKillTarget = ""
		cmds = append(cmds, m.refreshSessionsCmd())
		return m, tea.Batch(cmds...)
	default:
		// Any other key cancels.
		m.confirmKill = false
		m.confirmSession = nil
		m.sidebar.ConfirmKillTarget = ""
		return m, nil
	}
}

// killSessionCmd runs manager.Kill off the event loop, emitting an errMsg on
// failure so a tmux exec never blocks Update.
func (m Model) killSessionCmd(sess *session.Session) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if err := manager.Kill(sess); err != nil {
			return errMsg{err: fmt.Errorf("kill: %w", err)}
		}
		return nil
	}
}

// forkResumeCmd runs manager.ForkResume off the event loop, emitting an errMsg
// on failure.
func (m Model) forkResumeCmd(sess *session.Session, target string) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if err := manager.ForkResume(sess, target); err != nil {
			return errMsg{err: fmt.Errorf("fork-resume: %w", err)}
		}
		return nil
	}
}

// resumeCmd runs manager.Resume off the event loop, emitting an errMsg on
// failure.
func (m Model) resumeCmd(sess *session.Session, target string) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if err := manager.Resume(sess, target); err != nil {
			return errMsg{err: fmt.Errorf("resume: %w", err)}
		}
		return nil
	}
}

func (m Model) resumeSession(sess *session.Session) (tea.Model, tea.Cmd) {
	target := m.currentTmuxSession
	if target == "" {
		m.err = fmt.Errorf("resume: cannot determine current tmux session")
		m.statusbar.SetError("cannot determine current tmux session")
		return m, nil
	}
	// Run the resume exec in a Cmd so the tmux exec doesn't block the event
	// loop, then trigger an immediate session refresh so the new pane is
	// detected. Don't enter passthrough yet -- the new pane needs to be
	// discovered first.
	return m, tea.Batch(m.resumeCmd(sess, target), m.refreshSessionsCmd())
}

// openResumePicker opens the resume-target picker for a closed session. It lists
// the live tmux sessions (most-recently-active first) and pre-highlights the
// session naviClaude is running in. The actual resume happens on Enter in
// handleResumePickerKey.
func (m Model) openResumePicker(sess *session.Session) (tea.Model, tea.Cmd) {
	names := sessionNamesByRecency(m.tmuxClient.ListSessions())
	m.pendingResumeSession = sess
	m.resumePicker.SetSize(m.width, m.height)
	m.resumePicker.Show(names, m.currentTmuxSession)
	m.mode = ModeResumePicker
	m.statusbar.SetMode(ModeResumePicker.String())
	return m, m.resumePicker.Init()
}

// handleResumePickerKey drives the resume-target picker: typing filters,
// arrows move, Enter resumes into the chosen (or typed-new) tmux session, Esc
// cancels.
func (m Model) handleResumePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.resumePicker.Hide()
		m.pendingResumeSession = nil
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
		// Resume off the event loop, then refresh so the new pane is detected.
		// Don't enter passthrough yet -- the new pane needs discovery first.
		return m, tea.Batch(m.resumeInSessionCmd(sess, name), m.refreshSessionsCmd())

	default:
		var cmd tea.Cmd
		m.resumePicker, cmd = m.resumePicker.Update(msg)
		return m, cmd
	}
}

// resumeInSessionCmd runs manager.ResumeInSession off the event loop, emitting
// an errMsg on failure.
func (m Model) resumeInSessionCmd(sess *session.Session, targetName string) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if err := manager.ResumeInSession(sess, targetName); err != nil {
			return errMsg{err: fmt.Errorf("resume: %w", err)}
		}
		return nil
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

func (m Model) createNewSession() (tea.Model, tea.Cmd) {
	// Determine tmux session and CWD from the hovered item.
	var tmuxSess, cwd string

	sel := m.sidebar.SelectedSession()
	if sel != nil {
		tmuxSess = sel.TmuxSession
		cwd = sel.CWD
	}

	// If on a group header, use the group's tmux session name.
	if tmuxSess == "" {
		items := m.sidebar.FlatItems()
		cursor := m.sidebar.Cursor()
		if cursor >= 0 && cursor < len(items) && items[cursor].IsGroup {
			// Find the tmux session name from the group's first session.
			for _, item := range items[cursor+1:] {
				if item.IsGroup {
					break
				}
				if item.Session != nil {
					tmuxSess = item.Session.TmuxSession
					cwd = item.Session.CWD
					break
				}
			}
		}
	}

	// Fall back to the tmux session naviClaude is running in.
	if tmuxSess == "" {
		tmuxSess = m.currentTmuxSession
	}
	if tmuxSess == "" {
		m.err = fmt.Errorf("new session: cannot determine target tmux session")
		m.statusbar.SetError("cannot determine target tmux session")
		return m, nil
	}

	// Open the directory picker; the Claude window is created once a directory
	// is chosen (Enter). The current dir is preselected, so Enter keeps the
	// previous behavior of opening in the highlighted session's directory.
	base := cwd
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = home
		}
	}
	m.pendingDirAction = dirActionNewClaude
	m.pendingDirTmux = tmuxSess
	return m.openDirPicker(base, "New Claude session directory")
}

// createClaudeSessionIn launches a new Claude window in tmuxSess rooted at cwd.
func (m Model) createClaudeSessionIn(cwd, tmuxSess string) (tea.Model, tea.Cmd) {
	if tmuxSess == "" {
		m.err = fmt.Errorf("new session: cannot determine target tmux session")
		m.statusbar.SetError("cannot determine target tmux session")
		return m, nil
	}
	manager := m.manager
	claudeCmd := m.cfg.ClaudeCommand
	return m, func() tea.Msg {
		target, err := manager.CreateNewWithTarget(cwd, tmuxSess, claudeCmd)
		if err != nil {
			return newSessionMsg{err: err}
		}
		return newSessionMsg{
			tmuxTarget:  target,
			tmuxSession: tmuxSess,
			cwd:         cwd,
		}
	}
}

func (m Model) createNewTmuxSession() (tea.Model, tea.Cmd) {
	// Use the configured directory (or home) as the picker's starting base; the
	// picker handles ~ expansion. After a directory is chosen the flow continues
	// into the session-name input (see confirmDirSelection).
	cwd := m.cfg.NewSessionDir
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	m.pendingDirAction = dirActionNewTmux
	return m.openDirPicker(cwd, "New tmux session directory")
}

// openDirPicker activates the directory-picker overlay rooted at base. Show
// returns a command that loads the candidate pool off the Update goroutine, so
// the (possibly blocking) zoxide/subdir lookups don't freeze the UI.
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

// confirmDirSelection dispatches the chosen directory to the flow that opened
// the picker: create a Claude window (n), or continue to the name input for a
// new tmux session (N).
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
	case dirActionNewClaude:
		tmuxSess := m.pendingDirTmux
		m.pendingDirTmux = ""
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		return m.createClaudeSessionIn(dir, tmuxSess)

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
	claudeCmd := m.cfg.ClaudeCommand
	sessionName := strings.TrimSpace(name)
	return m, func() tea.Msg {
		tmuxSess, target, err := manager.CreateNewTmuxSession(cwd, claudeCmd, sessionName)
		if err != nil {
			return newTmuxSessionMsg{err: err}
		}
		return newTmuxSessionMsg{
			tmuxTarget:  target,
			tmuxSession: tmuxSess,
			cwd:         cwd,
		}
	}
}

func (m Model) executeContextAction(action string, sess *session.Session) (tea.Model, tea.Cmd) {
	if sess == nil {
		return m, nil
	}
	// Each case sets its own complete mode/statusbar target state so the result
	// is correct regardless of the caller's pre-set mode (the context-menu key
	// handler already reset to ModeList before dispatching here -- these cases
	// must not depend on that ordering).
	switch action {
	case "focus":
		if sess.Status == session.StatusClosed {
			return m.resumeSession(sess)
		}
		m.mode = ModePassthrough
		m.preview.SetPassthrough(true)
		m.statusbar.SetMode(ModePassthrough.String())
		return m, nil

	case "jump":
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		m.selectSessionInSidebar(sess)
		return m.jumpToPane()

	case "kill":
		// Mirror the keyboard kill path: optimistically remove the session for
		// instant feedback, run the actual kill async, and refresh.
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		target := sess.TmuxTarget
		m.removeSession(target)
		return m, tea.Batch(m.killSessionCmd(sess), m.refreshSessionsCmd())

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
		return m, loadDetailDataCmd(sess)

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

// enrichActiveSummaries fills in each active session's Summary from the history
// index (first prompt) when the session doesn't already carry one. No-op when
// scanner is nil.
func enrichActiveSummaries(scanner *session.HistoryScanner, active []*session.Session) {
	if scanner == nil {
		return
	}
	historyIndex, _ := scanner.LoadHistoryIndex()
	for _, s := range active {
		if s.ID != "" {
			if display, ok := historyIndex[s.ID]; ok && s.Summary == "" {
				s.Summary = display
			}
		}
	}
}

// scanClosedAndAllSessions returns the time-windowed closed sessions and the
// full closed-session list, excluding active IDs. It uses the session package's
// combined ScanClosedAndAll, which globs and parses every session file once and
// derives both views from that single pass (the closed set is a subset of all),
// avoiding the previous double Glob + double per-file parse. No-op (nil,nil)
// when scanner is nil.
func scanClosedAndAllSessions(scanner *session.HistoryScanner, closedHours float64, activeIDs map[string]bool) (closed, all []*session.Session) {
	if scanner == nil {
		return nil, nil
	}
	closed, all, _ = scanner.ScanClosedAndAll(closedHours, activeIDs)
	return closed, all
}

// refreshActiveCmd performs the fast active-session scan only (no history).
func (m Model) refreshActiveCmd() tea.Cmd {
	detector := m.detector
	scanner := m.historyScanner

	return func() tea.Msg {
		active, err := detector.Detect()
		if err != nil {
			return activeSessionsMsg{err: err}
		}
		enrichActiveSummaries(scanner, active)
		return activeSessionsMsg{active: active}
	}
}

// refreshHistoryCmd performs the slower closed/history scan.
func (m Model) refreshHistoryCmd() tea.Cmd {
	scanner := m.historyScanner
	closedHours := m.closedSessionHours
	activeSessions := m.activeSessions

	return func() tea.Msg {
		activeIDs := buildActiveIDSet(activeSessions)
		closed, all := scanClosedAndAllSessions(scanner, closedHours, activeIDs)
		return historySessionsMsg{
			closed: closed,
			all:    all,
		}
	}
}

// refreshSessionsCmd performs the full combined refresh (used by periodic ticker).
func (m Model) refreshSessionsCmd() tea.Cmd {
	detector := m.detector
	scanner := m.historyScanner
	closedHours := m.closedSessionHours

	return func() tea.Msg {
		active, err := detector.Detect()
		if err != nil {
			return sessionRefreshMsg{err: err}
		}

		activeIDs := buildActiveIDSet(active)
		closed, all := scanClosedAndAllSessions(scanner, closedHours, activeIDs)
		enrichActiveSummaries(scanner, active)

		return sessionRefreshMsg{
			active: active,
			closed: closed,
			all:    all,
		}
	}
}

func (m *Model) applySessionRefresh(msg sessionRefreshMsg) {
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

	// Update the preview header for the currently selected session.
	if sel := m.sidebar.SelectedSession(); sel != nil {
		m.selectPreviewSession(sel)
	}
}

// selectPreviewSession updates the preview header for the given session and
// clears stale content when the selection changes to a different pane target.
// It resizes the new pane's window to match the preview viewport and restores
// the previous pane's window to the full terminal size when navigating away.
func (m *Model) selectPreviewSession(s *session.Session) {
	target := ""
	if s != nil {
		target = s.TmuxTarget
	}

	if target != m.previewTarget {
		// Restore the previous window's width.
		if m.resizedWinTarget != "" && m.origWinW > 0 {
			m.tmuxClient.ResizeWindow(m.resizedWinTarget, m.origWinW, 0)
			m.resizedWinTarget = ""
		}

		m.previewTarget = target
		m.preview.SetContent("")
		m.preview.ResetScroll()

		// For subprocesses, show a styled informational message.
		if s != nil && s.Subprocess {
			parent := s.SubprocessParent
			if parent == "" {
				parent = "another program"
			}
			dim := lipgloss.NewStyle().Foreground(styles.ColorGray)
			accent := lipgloss.NewStyle().Foreground(styles.ColorBlue)
			hint := lipgloss.NewStyle().Foreground(styles.ColorAmber)
			m.preview.SetContent(fmt.Sprintf(
				"\n\n%s\n\n%s\n%s\n\n%s",
				dim.Italic(true).Render("  Preview unavailable"),
				dim.Render("  Claude is running inside "+accent.Render(parent)+dim.Render(".")),
				dim.Render("  The tmux pane belongs to "+accent.Render(parent)+dim.Render(", not Claude.")),
				hint.Render("  Press "+accent.Render(m.keys.Jump)+" to jump to the pane"),
			))
		}

		// Resize the new pane's window to the preview viewport size.
		// Skip subprocesses (pane belongs to parent app) and naviClaude's own window.
		naviWinTarget := m.currentTmuxWin
		if s != nil && s.TmuxTarget != "" && !s.Subprocess && tmux.WindowTarget(s.TmuxTarget) != naviWinTarget {
			winTarget := tmux.WindowTarget(s.TmuxTarget)
			// Save the original width so we can restore it when navigating away.
			// Only resize width -- changing height disrupts TUI layout (cursor
			// positioning, prompt duplication) in apps like Claude Code.
			if w, _, err := m.tmuxClient.WindowSize(winTarget); err == nil {
				m.origWinW = w
			} else {
				m.origWinW = m.width
			}
			m.resizedWinTarget = winTarget
			paneW, _ := m.previewPaneDimensions()
			m.tmuxClient.ResizeWindow(winTarget, paneW, 0)
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
		m.tmuxClient.ResizeWindow(m.resizedWinTarget, m.origWinW, 0)
		m.resizedWinTarget = ""
		m.origWinW = 0
	}
}

// ---------------------------------------------------------------------------
// Preview capture (async command)
// ---------------------------------------------------------------------------

func (m Model) capturePreviewCmd() tea.Cmd {
	sess := m.sidebar.SelectedSession()
	if sess == nil {
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
	pid := sess.PID
	sessionID := sess.ID
	captureEngine := m.captureEngine
	statusDetector := m.statusDetector
	// Compute the capture width here (on the event-loop goroutine, where the
	// dimensions are stable) and pass it to Capture, rather than relying on a
	// shared mutable field on the engine that the capture goroutine would race
	// with a concurrent resize. Mirrors resizeComponents: previewWidth-2 for the
	// preview's left/right padding.
	previewWidth := m.width - m.sidebarWidth()
	maxWidth := previewWidth - 2
	// Only overlay the pane cursor when the preview is focused (passthrough);
	// while browsing the menu the pane isn't receiving input, so a cursor block
	// would be misleading.
	showCursor := m.mode == ModePassthrough

	return func() tea.Msg {
		content, err := captureEngine.Capture(target, showCursor, maxWidth)
		if err != nil {
			return previewCaptureMsg{err: err, target: target, sessionID: sessionID}
		}

		// Claude Code's own native status is authoritative when available; fall
		// back to screen-content classification only when it is not, so the fast
		// preview path never overrides a reliable native status with a guess.
		status, ok := session.NativeStatus(pid, sessionID)
		if !ok {
			status = statusDetector.DetectFromContent(target, content)
		}

		return previewCaptureMsg{
			content:   content,
			target:    target,
			sessionID: sessionID,
			status:    status,
		}
	}
}

// loadClosedPreviewCmd loads conversation history from a closed session's .jsonl
// and formats it for display in the preview pane with colored styling.
func (m Model) loadClosedPreviewCmd(sess *session.Session) tea.Cmd {
	return func() tea.Msg {
		entries, err := session.LoadConversation(sess, 100)
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
				b.WriteString("\n  " + styles.ConversationAssistantLabel.Render("Claude") + "\n")
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
	previewWidth := m.width - sidebarWidth

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
	m.search.SetSize(sidebarWidth-1, contentHeight)
	m.help.SetSize(m.width, m.height)
	m.detail.SetSize(m.width, m.height)
	m.statsModel.SetSize(m.width, m.height)
	m.themePicker.SetSize(m.width, m.height)
	m.dirPicker.SetSize(m.width, m.height)
	m.resumePicker.SetSize(m.width, m.height)
	m.contextMenu.SetTermSize(m.width, m.height)
	// The capture width is computed per-capture in capturePreviewCmd and passed
	// to Capture, so there is no shared maxWidth field to set here. (Truncation
	// to the preview viewport width still happens -- see capturePreviewCmd.)
}

// previewPaneDimensions returns the width and height that managed tmux panes
// should be resized to so their content fits the preview viewport exactly.
func (m Model) previewPaneDimensions() (int, int) {
	sidebarWidth := m.sidebarWidth()
	previewWidth := m.width - sidebarWidth
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

func (m Model) renderTitleBar() string {
	left := styles.TitleBarName.Render(" naviClaude")
	info := styles.TitleBarDim.Render(fmt.Sprintf("%d sessions", m.sidebar.ActiveCount()))
	sep := styles.TitleBarDim.Render(" \u2022 ")

	leftLine := left + sep + info

	tmuxSess := m.currentTmuxSession
	if tmuxSess == "" {
		tmuxSess = "unknown"
	}
	right := styles.TitleBarDim.Render(tmuxSess + " ")

	leftWidth := lipgloss.Width(leftLine)
	rightWidth := lipgloss.Width(right)
	gap := m.width - leftWidth - rightWidth
	if gap < 1 {
		gap = 1
	}
	bar := leftLine + strings.Repeat(" ", gap) + right
	return styles.TitleBar.Width(m.width).Render(bar)
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

// ---------------------------------------------------------------------------
// Session selection helper
// ---------------------------------------------------------------------------

// selectSessionInSidebar iterates the sidebar sessions and moves the cursor
// to match the given session. This is a best-effort approach: we set sessions
// again with the target first so the sidebar rebuilds, but the simplest
// approach is to just set the sessions and let the cursor stay.
func (m *Model) selectSessionInSidebar(target *session.Session) {
	if target == nil {
		return
	}
	m.sidebar.SelectByID(target.ID)
	m.preview.SetSession(target)
}

// reloadMetricsForSelection clears stale metrics and reloads them when the
// selected session differs from the one the current metrics belong to. It
// returns the load command (or nil when the selection is unchanged or has no
// ID). Shared by list-mode and search-mode navigation so metrics never go
// stale on the search path.
func (m *Model) reloadMetricsForSelection(sel *session.Session) tea.Cmd {
	if sel == nil || sel.ID == "" || sel.ID == m.metricsSessionID {
		return nil
	}
	m.currentMetrics = nil
	m.metricsSessionID = sel.ID
	m.sidebar.SetMetrics(nil)
	m.preview.SetMetrics(nil)
	return loadMetricsCmd(sel)
}

// applyAliases overlays user-defined display-name aliases onto the given
// session list in place. Centralizes alias application so refresh/rename/search
// paths all behave identically.
func (m *Model) applyAliases(list []*session.Session) {
	aliases := m.aliasStore.All()
	if len(aliases) == 0 {
		return
	}
	for _, s := range list {
		if name, ok := aliases[s.ID]; ok {
			s.DisplayName = name
		}
	}
}

// mergePendingPlaceholder checks whether the pending new-session target has
// been detected in active. If found, clears pendingNewTarget. Otherwise,
// carries forward the placeholder from m.sessions so it stays visible.
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
			// Prepend the carried placeholder so it stays at the FRONT, matching
			// where creation originally placed (and selected) it. Appending would
			// make the new session visibly jump on the next refresh.
			return append([]*session.Session{s}, active...)
		}
	}
	return active
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
	// First try parsing $TMUX which has format "/tmp/tmux-1000/default,12345,0"
	// -- the session ID is the last component, but we need the name.
	// Easier: just ask tmux directly.
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// copyToClipboard attempts to copy text to the system clipboard.
// It tries pbcopy (macOS), then xclip, then xsel. Errors are silently ignored.
func copyToClipboard(text string) {
	// Try pbcopy (macOS).
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if cmd.Run() == nil {
		return
	}
	// Try xclip.
	cmd = exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = strings.NewReader(text)
	if cmd.Run() == nil {
		return
	}
	// Try xsel.
	cmd = exec.Command("xsel", "--clipboard", "--input")
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Run()
}

// overlayString composites the overlay text on top of the base text.
// This is a simplified overlay: it just returns base with overlay appended
// since lipgloss.Place handles proper centering for the help popup.
// For the context menu which uses margin-based positioning, we simply
// return the base as the menu is rendered via margin offsets.
func overlayString(base, overlay string, width, height int) string {
	// For Phase 1, the context menu is rendered below the main screen.
	// A proper overlay compositing can be added in Phase 2. For now,
	// we split both strings into lines and composite them.
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	// Pad base to fill height.
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}

	// The overlay uses MarginTop/MarginLeft for positioning. Parse the
	// overlay to find its actual content position. For simplicity in Phase 1,
	// render the overlay starting from its margin position.
	// Since lipgloss renders margins as leading newlines/spaces, the overlay
	// lines already contain the positioning. We just need to composite.
	for i, line := range overlayLines {
		if i < len(baseLines) && strings.TrimSpace(line) != "" {
			baseLines[i] = line
		}
	}

	// Truncate to height.
	if len(baseLines) > height {
		baseLines = baseLines[:height]
	}

	return strings.Join(baseLines, "\n")
}

// Ensure Model satisfies tea.Model at compile time.
var _ tea.Model = Model{}
