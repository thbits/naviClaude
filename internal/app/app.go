package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	content string
	target  string
	status  session.SessionStatus
	err     error
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

// Model is the top-level Bubble Tea model that composes all UI components
// and backend services.
type Model struct {
	// UI components
	sidebar     ui.SidebarModel
	preview     ui.PreviewModel
	statusbar   ui.StatusBarModel
	search      ui.SearchModel
	help        ui.HelpModel
	contextMenu ui.ContextMenuModel
	detail      ui.DetailModel
	statsModel  ui.StatsModel
	themePicker ui.ThemePickerModel
	nameInput   ui.NameInputModel

	// Backend services
	tmuxClient     *tmux.Client
	detector       *session.Detector
	historyScanner *session.HistoryScanner
	manager        *session.Manager
	captureEngine  *preview.CaptureEngine
	passthrough    *preview.Passthrough
	statusDetector *preview.StatusDetector

	// Application state
	mode           Mode
	width, height  int
	sessions       []*session.Session // active + recently-closed (for sidebar)
	allSessions    []*session.Session // all sessions (for search)
	activeSessions []*session.Session // just the active sessions (for progressive merge)
	err              error              // last error, shown briefly
	confirmKill      bool               // waiting for kill confirmation
	confirmSession   *session.Session   // session pending kill confirmation
	pendingNewTarget    string // tmux target of a just-created session; kept until detected
	pendingNewTmuxCWD   string // CWD for the tmux session being named
	previewTarget       string // tmux target currently shown in the preview panel

	// Stats cache
	statsCache *stats.Cache

	// Configuration
	cfg                config.Config
	keys               KeyMap
	sidebarWidthPct    int
	closedSessionHours float64
	processNames       []string
	currentTmuxSession string        // the tmux session naviClaude is running in
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
		sidebar:     ui.NewSidebar(30, 24),
		preview:     ui.NewPreview(50, 24),
		statusbar:   ui.NewStatusBar(80, version),
		search:      ui.NewSearch(),
		help:        ui.NewHelp(),
		contextMenu: ui.NewContextMenu(),
		detail:      ui.NewDetail(),
		statsModel:  ui.NewStats(),
		themePicker: ui.NewThemePicker(cfg.Theme),
		nameInput:   ui.NewNameInput(),

		// Backend
		tmuxClient:     tc,
		detector:       session.NewDetector(tc, cfg.ProcessNames, cfg.ActiveWindowSecs),
		historyScanner: hs,
		manager:        session.NewManager(tc),
		captureEngine:  preview.NewCaptureEngine(tc),
		passthrough:    preview.NewPassthrough(tc),
		statusDetector: preview.NewStatusDetector(),

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
		isPopup:            os.Getenv("TMUX_POPUP") != "",
		refreshInterval:    refreshInterval,
	}

	// Wire auto-collapse threshold.
	m.sidebar.SetCollapseAfterHours(cfg.CollapseAfterHours)

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

	case resourceRefreshMsg:
		m.handleResourceRefresh(msg)
		return m, nil

	case detailDataMsg:
		m.detail.SetData(msg.messageCount, msg.startTime)
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
		// was in flight, the result belongs to a different session.
		if sel := m.sidebar.SelectedSession(); sel == nil || sel.TmuxTarget != msg.target {
			return m, nil
		}
		if msg.err != nil {
			// Pane may have disappeared; not fatal.
			m.preview.SetContent(fmt.Sprintf("  Capture failed: %s", msg.err))
		} else if strings.TrimSpace(msg.content) == "" {
			// Empty pane content (session still loading or idle blank screen).
			m.preview.SetContent("  Waiting for session output...")
		} else {
			m.preview.SetContent(msg.content)
			// Only apply Waiting status from preview detection.
			// Active status is authoritative from the session detector;
			// applying Active from captures causes flickering due to
			// spinners, status bars, and cursor blink.
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

	return m, nil
}

// View renders the full UI. No loading overlay -- show the layout immediately.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
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

	if m.detail.IsVisible() {
		screen = ui.PlaceOverlay(screen, m.detail.View())
	}

	if m.help.IsVisible() {
		screen = ui.PlaceOverlay(screen, m.help.View())
	}

	if m.contextMenu.IsVisible() {
		// The context menu positions itself via margin; overlay it.
		menuView := m.contextMenu.View()
		screen = lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top,
			screen,
			lipgloss.WithWhitespaceChars(" "),
		)
		// Composite: render menu on top of screen at its position.
		// Since the menu uses MarginLeft/MarginTop for positioning, we just
		// join it over the screen. A simple approach: replace screen with an
		// overlay.
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
	}
	return m, nil
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case m.keys.Quit:
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
			// Show conversation history in the preview pane.
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
		m.sidebar, cmd = m.sidebar.Update(msg)
		// After navigation, update preview session header.
		if sel := m.sidebar.SelectedSession(); sel != nil {
			m.selectPreviewSession(sel)
			// Auto-load conversation preview for closed sessions.
			if sel.Status == session.StatusClosed {
				return m, tea.Batch(cmd, m.loadClosedPreviewCmd(sel))
			}
		} else if groupName := m.sidebar.SelectedGroupName(); groupName != "" {
			// Show group summary when hovering on a group header.
			m.preview.SetGroupSummary(groupName, m.sidebar.GroupSessions(groupName))
		}
		return m, cmd
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
		m.sidebar, cmd = m.sidebar.Update(msg)
		if sel := m.sidebar.SelectedSession(); sel != nil {
			m.selectPreviewSession(sel)
			if sel.Status == session.StatusClosed {
				return m, tea.Batch(cmd, m.loadClosedPreviewCmd(sel))
			}
		}
		return m, cmd

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
		if m.confirmSession != nil {
			if err := m.manager.Kill(m.confirmSession); err != nil {
				m.err = fmt.Errorf("kill: %w", err)
			}
			// Remove the session from the list immediately for instant feedback.
			m.removeSession(m.confirmSession.TmuxTarget)
		}
		m.confirmKill = false
		m.confirmSession = nil
		m.sidebar.ConfirmKillTarget = ""
		return m, m.refreshSessionsCmd()
	default:
		// Any other key cancels.
		m.confirmKill = false
		m.confirmSession = nil
		m.sidebar.ConfirmKillTarget = ""
		return m, nil
	}
}

func (m Model) resumeSession(sess *session.Session) (tea.Model, tea.Cmd) {
	target := m.currentTmuxSession
	if target == "" {
		m.err = fmt.Errorf("resume: cannot determine current tmux session")
		m.statusbar.SetError("cannot determine current tmux session")
		return m, nil
	}
	if err := m.manager.Resume(sess, target); err != nil {
		m.err = fmt.Errorf("resume: %w", err)
		return m, nil
	}
	// Trigger an immediate session refresh so the new pane is detected.
	// Don't enter passthrough yet -- the new pane needs to be discovered first.
	return m, m.refreshSessionsCmd()
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

	manager := m.manager
	return m, func() tea.Msg {
		target, err := manager.CreateNewWithTarget(cwd, tmuxSess)
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
	// Use configured directory, or fall back to home dir.
	cwd := m.cfg.NewSessionDir
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	// Expand ~ prefix.
	if strings.HasPrefix(cwd, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = filepath.Join(home, cwd[2:])
		}
	} else if cwd == "~" {
		cwd, _ = os.UserHomeDir()
	}
	m.pendingNewTmuxCWD = cwd
	m.mode = ModeNameInput
	m.nameInput.SetSize(m.sidebarWidth())
	cmd := m.nameInput.Activate()
	m.statusbar.SetMode(ModeNameInput.String())
	return m, cmd
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
		m.selectSessionInSidebar(sess)
		return m.jumpToPane()

	case "kill":
		if err := m.manager.Kill(sess); err != nil {
			m.err = fmt.Errorf("kill: %w", err)
		}
		return m, nil

	case "resume":
		return m.resumeSession(sess)

	case "fork_resume":
		target := m.currentTmuxSession
		if target == "" {
			m.err = fmt.Errorf("fork-resume: cannot determine current tmux session")
			return m, nil
		}
		if err := m.manager.ForkResume(sess, target); err != nil {
			m.err = fmt.Errorf("fork-resume: %w", err)
		}
		return m, nil

	case "detail":
		m.detail.Show(sess)
		m.mode = ModeDetail
		m.statusbar.SetMode(ModeDetail.String())
		return m, loadDetailDataCmd(sess)

	case "copy_id":
		// Best-effort clipboard copy; silently ignore errors.
		if sess.ID != "" {
			copyToClipboard(sess.ID)
		}
		return m, nil

	case "copy_path":
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

// refreshActiveCmd performs the fast active-session scan only (no history).
func (m Model) refreshActiveCmd() tea.Cmd {
	detector := m.detector
	scanner := m.historyScanner

	return func() tea.Msg {
		active, err := detector.Detect()
		if err != nil {
			return activeSessionsMsg{err: err}
		}

		// Enrich active sessions with history data (summary).
		if scanner != nil {
			historyIndex, _ := scanner.LoadHistoryIndex()
			for _, s := range active {
				if s.ID != "" {
					if display, ok := historyIndex[s.ID]; ok && s.Summary == "" {
						s.Summary = display
					}
				}
			}
		}

		return activeSessionsMsg{active: active}
	}
}

// refreshHistoryCmd performs the slower closed/history scan.
func (m Model) refreshHistoryCmd() tea.Cmd {
	scanner := m.historyScanner
	closedHours := m.closedSessionHours
	activeSessions := m.activeSessions

	return func() tea.Msg {
		activeIDs := make(map[string]bool, len(activeSessions))
		for _, s := range activeSessions {
			if s.ID != "" {
				activeIDs[s.ID] = true
			}
		}

		var closed, all []*session.Session
		if scanner != nil {
			closed, _ = scanner.ScanClosed(closedHours, activeIDs)
			all, _ = scanner.ScanAll(activeIDs)
		}

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

		activeIDs := make(map[string]bool, len(active))
		for _, s := range active {
			if s.ID != "" {
				activeIDs[s.ID] = true
			}
		}

		var closed, all []*session.Session
		if scanner != nil {
			closed, _ = scanner.ScanClosed(closedHours, activeIDs)
			all, _ = scanner.ScanAll(activeIDs)
		}

		// Enrich active sessions with history data.
		if scanner != nil {
			historyIndex, _ := scanner.LoadHistoryIndex()
			for _, s := range active {
				if s.ID != "" {
					if display, ok := historyIndex[s.ID]; ok && s.Summary == "" {
						s.Summary = display
					}
				}
			}
		}

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
func (m *Model) selectPreviewSession(s *session.Session) {
	target := ""
	if s != nil {
		target = s.TmuxTarget
	}
	if target != m.previewTarget {
		m.previewTarget = target
		m.preview.SetContent("")
	}
	if s != nil {
		m.preview.SetSession(s)
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

	target := sess.TmuxTarget
	captureEngine := m.captureEngine
	statusDetector := m.statusDetector

	return func() tea.Msg {
		content, err := captureEngine.Capture(target)
		if err != nil {
			return previewCaptureMsg{err: err, target: target}
		}

		status := statusDetector.DetectFromContent(target, content)

		return previewCaptureMsg{
			content: content,
			target:  target,
			status:  status,
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
	m.contextMenu.SetTermSize(m.width, m.height)
	// Truncate captured pane lines to the preview viewport width to prevent
	// overflow when the source pane is wider than the popup.
	m.captureEngine.SetMaxWidth(previewWidth - 2) // -2 for left padding
}

func (m Model) renderTitleBar() string {
	left := styles.TitleBarName.Render(" naviClaude")
	info := styles.TitleBarDim.Render(fmt.Sprintf("%d sessions", m.sidebar.ActiveCount()))
	sep := styles.TitleBarDim.Render(" | ")

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
			return append(active, s)
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
