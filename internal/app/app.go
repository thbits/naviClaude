package app

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tomhalo/naviclaude/internal/preview"
	"github.com/tomhalo/naviclaude/internal/session"
	"github.com/tomhalo/naviclaude/internal/tmux"
	"github.com/tomhalo/naviclaude/internal/ui"
)

// tickPreviewMsg triggers a preview capture refresh.
type tickPreviewMsg struct{}

// tickSessionMsg triggers a full session re-scan.
type tickSessionMsg struct{}

// sessionRefreshMsg carries the results of an async session refresh.
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

	// Backend services
	tmuxClient     *tmux.Client
	detector       *session.Detector
	historyScanner *session.HistoryScanner
	manager        *session.Manager
	captureEngine  *preview.CaptureEngine
	passthrough    *preview.Passthrough
	statusDetector *preview.StatusDetector

	// Application state
	mode          Mode
	width, height int
	sessions      []*session.Session // active + recently-closed (for sidebar)
	allSessions   []*session.Session // all sessions (for search)
	err           error              // last error, shown briefly

	// Configuration (hardcoded for Phase 1)
	sidebarWidthPct    int
	closedSessionHours float64
	processNames       []string
	currentTmuxSession string // the tmux session naviClaude is running in
}

// New creates a fully-wired Model ready to be passed to tea.NewProgram.
func New() Model {
	tc := tmux.New()
	hs, _ := session.NewHistoryScanner("")

	m := Model{
		// UI
		sidebar:     ui.NewSidebar(30, 24),
		preview:     ui.NewPreview(50, 24),
		statusbar:   ui.NewStatusBar(80),
		search:      ui.NewSearch(),
		help:        ui.NewHelp(),
		contextMenu: ui.NewContextMenu(),

		// Backend
		tmuxClient:     tc,
		detector:       session.NewDetector(tc, nil),
		historyScanner: hs,
		manager:        session.NewManager(tc),
		captureEngine:  preview.NewCaptureEngine(tc),
		passthrough:    preview.NewPassthrough(tc),
		statusDetector: preview.NewStatusDetector(tc),

		// Defaults
		mode:               ModeList,
		sidebarWidthPct:    30,
		closedSessionHours: 6,
		processNames:       []string{"claude"},
		currentTmuxSession: detectCurrentTmuxSession(),
	}
	return m
}

// Init checks that tmux is available and starts the ticker commands.
func (m Model) Init() tea.Cmd {
	if !m.tmuxClient.IsRunning() {
		m.err = fmt.Errorf("tmux is not running -- naviClaude requires an active tmux server")
		return tea.Quit
	}
	if err := m.tmuxClient.CheckVersion(3, 2); err != nil {
		m.err = err
		return tea.Quit
	}
	return tea.Batch(
		tickPreview(),
		tickSession(),
		m.refreshSessionsCmd(),
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
		cmds = append(cmds, tickPreview())
		cmds = append(cmds, m.capturePreviewCmd())
		return m, tea.Batch(cmds...)

	case tickSessionMsg:
		cmds = append(cmds, tickSession())
		cmds = append(cmds, m.refreshSessionsCmd())
		return m, tea.Batch(cmds...)

	// -- Async results -------------------------------------------------------
	case sessionRefreshMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.applySessionRefresh(msg)
		}
		return m, nil

	case previewCaptureMsg:
		if msg.err != nil {
			// Pane may have disappeared; not fatal.
			m.preview.SetContent(fmt.Sprintf("  Capture failed: %s", msg.err))
		} else {
			m.preview.SetContent(msg.content)
			// Update session status in our local list.
			m.updateSessionStatus(msg.target, msg.status)
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

// View renders the full UI.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	sidebarWidth := m.sidebarWidth()
	previewWidth := m.width - sidebarWidth

	// Build the sidebar column. If search is active, stack the search input
	// above the sidebar.
	var sidebarView string
	if m.search.IsActive() {
		searchView := m.search.View()
		searchHeight := lipgloss.Height(searchView)
		remainingHeight := m.height - 1 - searchHeight // 1 for status bar
		if remainingHeight < 1 {
			remainingHeight = 1
		}
		m.sidebar.SetSize(sidebarWidth, remainingHeight)
		sidebarView = lipgloss.JoinVertical(lipgloss.Left, searchView, m.sidebar.View())
	} else {
		sidebarView = m.sidebar.View()
	}

	previewView := m.preview.View()

	// Ensure both columns are the correct height (height - 1 for status bar).
	contentHeight := m.height - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	sidebarView = lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(contentHeight).
		MaxHeight(contentHeight).
		Render(sidebarView)

	previewView = lipgloss.NewStyle().
		Width(previewWidth).
		Height(contentHeight).
		MaxHeight(contentHeight).
		Render(previewView)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, previewView)

	statusView := m.statusbar.View()
	screen := lipgloss.JoinVertical(lipgloss.Left, mainContent, statusView)

	// Render overlays on top.
	if m.help.IsVisible() {
		helpView := m.help.View()
		screen = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, helpView)
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
	key := msg.String()

	// Global quit.
	if key == KeyCtrlC {
		return m, tea.Quit
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
	}
	return m, nil
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case KeyQuit:
		return m, tea.Quit

	case KeyHelp:
		m.help.Toggle()
		if m.help.IsVisible() {
			m.mode = ModeHelp
		}
		return m, nil

	case KeySearch:
		m.mode = ModeSearch
		m.search.SetSessions(m.allSessions)
		m.search.Activate()
		m.statusbar.SetMode(ModeSearch.String())
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
			return m.resumeSession(sess)
		}
		// Enter passthrough mode.
		m.mode = ModePassthrough
		m.preview.SetPassthrough(true)
		m.statusbar.SetMode(ModePassthrough.String())
		return m, nil

	case KeyFocus:
		return m.jumpToPane()

	case KeyKill:
		return m.killSelected()

	case KeyNew:
		// Phase 1: show a brief message; full implementation in a later phase.
		m.err = fmt.Errorf("new session: not yet implemented (Phase 2)")
		return m, nil

	case KeyDetail:
		m.err = fmt.Errorf("detail view: not yet implemented (Phase 2)")
		return m, nil

	case KeyStats:
		m.err = fmt.Errorf("stats view: not yet implemented (Phase 2)")
		return m, nil

	default:
		// Navigation keys: delegate to sidebar.
		var cmd tea.Cmd
		m.sidebar, cmd = m.sidebar.Update(msg)
		// After navigation, update preview session header.
		if sel := m.sidebar.SelectedSession(); sel != nil {
			m.preview.SetSession(sel)
		}
		return m, cmd
	}
}

func (m Model) handlePassthroughKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case KeyExitPassthrough, KeyExitPassthrough2:
		m.mode = ModeList
		m.preview.SetPassthrough(false)
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
			m.statusbar.SetMode(ModeList.String())
			return m, nil
		}
		if err := m.passthrough.SendKey(sess.TmuxTarget, msg); err != nil {
			// Send-keys failed; session may have died.
			m.mode = ModeList
			m.preview.SetPassthrough(false)
			m.statusbar.SetMode(ModeList.String())
			m.err = fmt.Errorf("passthrough send failed: %w", err)
		}
		return m, nil
	}
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case KeySearchSelect:
		// Select the highlighted result and return to list mode.
		sel := m.search.SelectedResult()
		m.search.Deactivate()
		m.mode = ModeList
		m.statusbar.SetMode(ModeList.String())
		if sel != nil {
			// Update sidebar to show this session selected.
			m.selectSessionInSidebar(sel)
			m.preview.SetSession(sel)
		}
		return m, nil

	default:
		// Delegate everything else (typing, Esc, up/down) to the search model.
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		// If search was deactivated by Esc, return to list mode.
		if !m.search.IsActive() {
			m.mode = ModeList
			m.statusbar.SetMode(ModeList.String())
		}
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
				m.preview.SetSession(sel)
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
	if sess == nil {
		return m, nil
	}
	if err := m.manager.Kill(sess); err != nil {
		m.err = fmt.Errorf("kill: %w", err)
	}
	return m, nil
}

func (m Model) resumeSession(sess *session.Session) (tea.Model, tea.Cmd) {
	target := m.currentTmuxSession
	if target == "" {
		m.err = fmt.Errorf("resume: cannot determine current tmux session")
		return m, nil
	}
	if err := m.manager.Resume(sess, target); err != nil {
		m.err = fmt.Errorf("resume: %w", err)
		return m, nil
	}
	// Enter passthrough mode; the session will appear on next refresh.
	m.mode = ModePassthrough
	m.preview.SetPassthrough(true)
	m.statusbar.SetMode(ModePassthrough.String())
	return m, nil
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
		m.err = fmt.Errorf("detail view: not yet implemented (Phase 2)")
		return m, nil

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
// Session refresh (async command)
// ---------------------------------------------------------------------------

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
	// Combine active and closed for the sidebar.
	combined := make([]*session.Session, 0, len(msg.active)+len(msg.closed))
	combined = append(combined, msg.active...)
	combined = append(combined, msg.closed...)
	m.sessions = combined
	m.sidebar.SetSessions(combined)

	// All sessions (active + all closed) for search.
	allCombined := make([]*session.Session, 0, len(msg.active)+len(msg.all))
	allCombined = append(allCombined, msg.active...)
	allCombined = append(allCombined, msg.all...)
	m.allSessions = allCombined
	m.search.SetSessions(allCombined)

	// Update the preview header for the currently selected session.
	if sel := m.sidebar.SelectedSession(); sel != nil {
		m.preview.SetSession(sel)
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
		// No pane to capture for a closed session.
		return func() tea.Msg {
			return previewCaptureMsg{
				content: "  Session is closed. Press Enter to resume.",
				target:  "",
				status:  session.StatusClosed,
			}
		}
	}

	target := sess.TmuxTarget
	captureEngine := m.captureEngine
	statusDetector := m.statusDetector

	return func() tea.Msg {
		content, err := captureEngine.Capture(target)
		if err != nil {
			return previewCaptureMsg{err: err, target: target}
		}

		status, statusErr := statusDetector.Detect(target)
		if statusErr != nil {
			status = session.StatusActive // fallback
		}

		return previewCaptureMsg{
			content: content,
			target:  target,
			status:  status,
		}
	}
}

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

func (m *Model) resizeComponents() {
	sidebarWidth := m.sidebarWidth()
	previewWidth := m.width - sidebarWidth
	contentHeight := m.height - 1 // status bar
	if contentHeight < 1 {
		contentHeight = 1
	}

	m.sidebar.SetSize(sidebarWidth, contentHeight)
	m.preview.SetSize(previewWidth, contentHeight)
	m.statusbar.SetSize(m.width)
	m.search.SetSize(sidebarWidth, contentHeight)
	m.help.SetSize(m.width, m.height)
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
	m.preview.SetSession(target)
	// The sidebar does not expose a SelectByID method, so we rebuild sessions
	// and rely on the cursor staying on the closest match. For Phase 1 this is
	// acceptable; Phase 2 can add explicit cursor control.
}

// updateSessionStatus updates the status of a session in the local list by
// tmux target.
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

func tickPreview() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return tickPreviewMsg{}
	})
}

func tickSession() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
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

