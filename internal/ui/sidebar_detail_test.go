package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/thbits/naviClaude/internal/attention"
	"github.com/thbits/naviClaude/internal/harness/claude"
	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/internal/styles"
)

func plainSegments(segs []segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.text)
	}
	return b.String()
}

func TestActivitySegments(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	active := &session.Session{Status: session.StatusActive}
	waiting := &session.Session{Status: session.StatusWaiting}

	tests := []struct {
		name string
		s    *session.Session
		a    *session.SessionActivity
		want string
	}{
		{"working tool", active, &session.SessionActivity{Tool: "Bash", Target: "go test ./...", TurnStart: ago(42 * time.Second)},
			"Bash · go test ./... · 42s"},
		{"working edit", active, &session.SessionActivity{Tool: "Edit", Target: "sidebar.go", TurnStart: ago(3 * time.Second)},
			"Edit · sidebar.go · 3s"},
		{"single agent shows no suffix", active, &session.SessionActivity{Tool: "Agent", Target: "Fix wrong context", TurnStart: ago(2 * time.Minute), Agents: 1},
			"Agent · Fix wrong context · 2m"},
		{"thinking with agents", active, &session.SessionActivity{TurnStart: ago(42 * time.Second), Agents: 3},
			styles.IconAgent + " 3 agents · " + styles.IconThinking + " thinking · 42s"},
		{"only background agents left", active, &session.SessionActivity{TurnStart: ago(18 * time.Minute), TurnEnded: true, Agents: 4},
			styles.IconAgent + " 4 agents running"},
		{"agents and shells left", active, &session.SessionActivity{TurnEnded: true, Agents: 1, Shells: 2},
			styles.IconAgent + " 1 agent · " + styles.IconShell + " 2 shells running"},
		{"only shells left", active, &session.SessionActivity{TurnEnded: true, Shells: 1}, styles.IconShell + " 1 shell running"},
		{"monitor apart from shells", active, &session.SessionActivity{TurnStart: ago(42 * time.Second), Agents: 2, Shells: 8, Monitors: 1},
			styles.IconAgent + " 2 agents · " + styles.IconShell + " 8 shells · " + styles.IconMonitor + " 1 monitor · " + styles.IconThinking + " thinking · 42s"},
		{"only a monitor left", active, &session.SessionActivity{TurnEnded: true, Monitors: 2}, styles.IconMonitor + " 2 monitors running"},
		{"working with a shell", active, &session.SessionActivity{Tool: "Read", Target: "a.go", TurnStart: ago(5 * time.Second), Shells: 1},
			styles.IconShell + " 1 shell · Read · a.go · 5s"},
		{"approximate turn start", active, &session.SessionActivity{TurnStart: ago(75 * time.Minute), TurnStartApprox: true},
			styles.IconThinking + " thinking · 1h15m+"},
		{"active before load", active, nil, "working"},
		{"restoring", &session.Session{Status: session.StatusIdle, Restoring: true}, &session.SessionActivity{Tool: "Bash"}, "↻ restoring"},
		{"needs approval", waiting, &session.SessionActivity{Tool: "Bash", Target: "rm -rf build"},
			"⏸ needs approval: Bash rm -rf build"},
		{"question", waiting, &session.SessionActivity{Tool: "AskUserQuestion", Target: "Which approach?"},
			"⏸ question: Which approach?"},
		{"question while status lags", active, &session.SessionActivity{Tool: "AskUserQuestion"}, "⏸ question"},
		{"plan approval", waiting, &session.SessionActivity{Tool: "ExitPlanMode"}, "⏸ plan approval"},
		{"waiting without tool", &session.Session{Status: session.StatusWaiting, WaitingFor: "permission prompt"}, nil,
			"⏸ permission prompt"},
		{"done", &session.Session{Status: session.StatusIdle, LastActivity: ago(time.Hour)},
			&session.SessionActivity{LastAssistant: ago(5 * time.Minute)}, "✓ done 5m ago"},
		{"done just now with background agent", &session.Session{Status: session.StatusIdle},
			&session.SessionActivity{LastAssistant: ago(10 * time.Second), Agents: 1}, "✓ done just now · " + styles.IconAgent + " 1 agent"},
		{"done falls back to last activity", &session.Session{Status: session.StatusIdle, Summary: "fix it", LastActivity: ago(3 * time.Hour)}, nil,
			"✓ done 3h ago"},
		{"new session before any transcript", &session.Session{Status: session.StatusIdle, LastActivity: ago(time.Second)}, nil,
			"○ new session"},
		{"new session with an empty transcript", &session.Session{Status: session.StatusIdle}, &session.SessionActivity{},
			"○ new session"},
		{"closed", &session.Session{Status: session.StatusClosed, LastActivity: ago(48 * time.Hour)}, nil, "closed 2d ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := plainSegments(activitySegments(tt.s, tt.a, now)); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The flexible target shrinks first so agents and elapsed time stay visible.
func TestRenderSegmentsShrinksTarget(t *testing.T) {
	now := time.Now()
	segs := activitySegments(&session.Session{Status: session.StatusActive},
		&session.SessionActivity{Tool: "Bash", Target: "go test -race -count=1 ./internal/...", TurnStart: now.Add(-42 * time.Second), Agents: 3}, now)
	got := ansi.Strip(renderSegments(segs, styles.ColorSelectionDim, 32))
	if want := styles.IconAgent + " 3 agents · Bash · go te… · 42s"; got != want {
		t.Errorf("got %q (%d cells), want %q", got, ansi.StringWidth(got), want)
	}
	if got := ansi.StringWidth(renderSegments(segs, styles.ColorSelectionDim, 12)); got > 12 {
		t.Errorf("hard cut overflowed: %d cells", got)
	}
}

func TestCtxBarColor(t *testing.T) {
	tests := []struct {
		pct  float64
		want lipgloss.Color
	}{
		{0, styles.ColorCyan},
		{0.59, styles.ColorCyan},
		{0.60, styles.ColorAmber},
		{0.85, styles.ColorAmber},
		{0.86, styles.ColorRed},
		{1, styles.ColorRed},
	}
	for _, tt := range tests {
		if got := ctxBarColor(tt.pct); got != tt.want {
			t.Errorf("ctxBarColor(%v) = %v, want %v", tt.pct, got, tt.want)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	tests := map[int]string{
		950:       "950",
		62_400:    "62K",
		200_000:   "200K",
		1_000_000: "1M",
		1_500_000: "1.5M",
		2_000_000: "2M",
	}
	for n, want := range tests {
		if got := formatTokens(n); got != want {
			t.Errorf("formatTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestCtxLine(t *testing.T) {
	got := ansi.Strip(ctxLine(62_000, 1_000_000, styles.ColorSelectionDim, 36))
	if !strings.HasPrefix(got, "ctx ") || !strings.HasSuffix(got, " 62K/1M (6%)") {
		t.Errorf("ctxLine = %q", got)
	}
	if ctxLine(0, 1_000_000, styles.ColorSelectionDim, 36) != "" {
		t.Error("no usage should render no line")
	}
	for _, w := range []int{10, 26, 36, 56} {
		if lw := ansi.StringWidth(ctxLine(950_000, 1_000_000, styles.ColorSelectionDim, w)); lw > w {
			t.Errorf("width %d: ctx line is %d cells", w, lw)
		}
	}
}

func TestRepoSegments(t *testing.T) {
	if got := plainSegments(repoSegments(session.GitRef{Branch: "main"}, nil, 80)); got != "⎇ main" {
		t.Errorf("no files: %q", got)
	}
	files := []session.ChangedFile{{Path: "a", Added: 100, Removed: 3}, {Path: "b", Added: 20, Removed: 5}}
	ref := session.GitRef{Repo: "naviClaude", Branch: "main"}
	if got := plainSegments(repoSegments(ref, files, 80)); got != styles.IconRepo+" naviClaude ⎇ main · 2 files +120/−8" {
		t.Errorf("files: %q", got)
	}
	// Too narrow for both: the branch keeps its room and the repo goes.
	if got := plainSegments(repoSegments(ref, files, 30)); got != "⎇ main · 2 files +120/−8" {
		t.Errorf("narrow: %q", got)
	}
}

// Every detail line must fit the pane: a wrapped line would shift the hit map
// and every row below it.
func TestSelectedDetailLinesNeverWrap(t *testing.T) {
	now := time.Now()
	for _, width := range []int{35, 42, 60} {
		sb := NewSidebar(width, 30)
		sb.SetSize(width, 30)
		sess := &session.Session{ID: "1", DisplayName: "one", TmuxSession: "alpha", TmuxTarget: "alpha:0.0",
			CWD: "/repo", Status: session.StatusActive, LastActivity: now, Summary: strings.Repeat("summary ", 20)}
		sb.SetSessions([]*session.Session{sess})
		sb.SetCursor(1)
		sb.SetMetrics("1", &session.SessionMetrics{TokensUsed: 900_000, ContextLimit: 1_000_000})
		sb.SetActivity("1", &session.SessionActivity{Tool: "Bash", Target: strings.Repeat("very long command ", 10),
			TurnStart: now.Add(-42 * time.Second), Agents: 4})
		sb.SetBranch("/repo", session.GitRef{Repo: "a-long-repository-name", Branch: "feature/a-really-long-branch-name-for-testing"}, true)
		sb.SetChangedFiles("1", []session.ChangedFile{{Path: "x", Added: 12345, Removed: 678}})

		lines := strings.Split(sb.View(), "\n")
		// header, group, name, summary, activity, ctx, repo
		if len(lines) < 7 {
			t.Fatalf("width %d: %d lines, want >= 7:\n%s", width, len(lines), sb.View())
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				t.Errorf("width %d: line %d is %d cells: %q", width, i, w, ansi.Strip(l))
			}
		}
		body := ansi.Strip(sb.View())
		for _, want := range []string{styles.IconAgent + " 4 agents · Bash", "ctx ", "⎇ feature"} {
			if !strings.Contains(body, want) {
				t.Errorf("width %d: missing %q in\n%s", width, want, body)
			}
		}
		// The hit map covers every rendered line of the selected row.
		for y := 2; y <= 6; y++ {
			if hit := sb.HitTest(3, y); hit.Kind != HitSession || hit.Session.ID != "1" {
				t.Errorf("width %d: HitTest(y=%d) = %+v, want session 1", width, y, hit)
			}
		}
	}
}

// A late activity load for a previously selected session must not render
// under the current one.
func TestActivityIgnoredForOtherSession(t *testing.T) {
	sb := NewSidebar(40, 20)
	sb.SetSize(40, 20)
	sb.SetSessions([]*session.Session{{ID: "1", DisplayName: "one", TmuxSession: "a", Status: session.StatusActive, LastActivity: time.Now()}})
	sb.SetCursor(1)
	sb.SetActivity("other", &session.SessionActivity{Tool: "Bash", Target: "stale"})
	if strings.Contains(ansi.Strip(sb.View()), "stale") {
		t.Error("activity for another session was rendered")
	}
}

func groupOrder(sb SidebarModel) []string {
	var out []string
	for _, it := range sb.FlatItems() {
		if it.IsGroup {
			out = append(out, "#")
		} else {
			out = append(out, it.Session.ID)
		}
	}
	return out
}

func TestAttentionFirstOrdering(t *testing.T) {
	now := time.Now()
	mk := func(id, group string, st session.SessionStatus, age time.Duration) *session.Session {
		return &session.Session{ID: id, DisplayName: id, TmuxSession: group, TmuxTarget: group + ":" + id,
			Status: st, LastActivity: now.Add(-age)}
	}
	for _, order := range []string{"name", "activity"} {
		t.Run(order, func(t *testing.T) {
			sb := NewSidebar(40, 40)
			sb.SetSize(40, 40)
			sb.SetGroupSortOrder(order)
			sb.SetSessionSortOrder(order)
			sessions := []*session.Session{
				mk("a1", "alpha", session.StatusActive, time.Minute),
				mk("a2", "alpha", session.StatusIdle, 2*time.Minute),
				mk("z1", "zulu", session.StatusIdle, time.Hour),
				mk("z2", "zulu", session.StatusWaiting, 2*time.Hour),
				{ID: "c1", DisplayName: "c1", Status: session.StatusClosed, LastActivity: now},
			}
			sb.SetSessions(sessions)
			sb.SetCursor(cursorOf(t, &sb, "a2"))

			got := strings.Join(groupOrder(sb), " ")
			// zulu holds the waiting session so it leads, with z2 on top; Closed stays last.
			if want := "# z2 z1 # a1 a2 #"; got != want {
				t.Errorf("order = %q, want %q", got, want)
			}

			// a1 now needs the user: alpha moves up, but the cursor stays on a2.
			sessions[0] = mk("a1", "alpha", session.StatusWaiting, time.Minute)
			sb.SetSessions(sessions)
			if sel := sb.SelectedSession(); sel == nil || sel.ID != "a2" {
				t.Fatalf("selection jumped to %+v, want a2", sel)
			}
			// Both groups now wait, so the configured order decides between them.
			if got, want := strings.Join(groupOrder(sb), " "), "# a1 a2 # z2 z1 #"; got != want {
				t.Errorf("after flip: order = %q, want %q", got, want)
			}
		})
	}
}

func cursorOf(t *testing.T, m *SidebarModel, id string) int {
	t.Helper()
	for i, it := range m.flatItems {
		if !it.isGroup && it.session.ID == id {
			return i
		}
	}
	t.Fatalf("session %q not in sidebar", id)
	return -1
}

func detailSidebar(t *testing.T) *SidebarModel {
	t.Helper()
	sb := NewSidebar(60, 30)
	sb.SetSize(60, 30)
	sb.SetSessions([]*session.Session{{ID: "1", DisplayName: "one", TmuxSession: "alpha", TmuxTarget: "alpha:0.0",
		CWD: "/repo", Model: "sonnet", Status: session.StatusActive, LastActivity: time.Now()}})
	sb.SetCursor(1)
	sb.SetContextLimitFunc(func(s *session.Session, modelID string, tokens int) int {
		return claude.New(claude.Options{}).ContextLimit(modelID, s.Model, tokens)
	})
	return &sb
}

// Metrics loaded for another session (or before a placeholder) must not draw
// their context bar on the selected row.
func TestCtxBarIgnoresOtherSessionsMetrics(t *testing.T) {
	sb := detailSidebar(t)
	sb.SetMetrics("other", &session.SessionMetrics{TokensUsed: 150_000, ContextLimit: 200_000})
	if body := ansi.Strip(sb.View()); strings.Contains(body, "ctx ") {
		t.Errorf("selected row shows another session's ctx bar:\n%s", body)
	}
}

// The limit follows the fresher tail read rather than the one-off full scan.
func TestCtxLimitResolvedAtRender(t *testing.T) {
	tests := []struct {
		name string
		act  session.SessionActivity
		want string
	}{
		{"tail usage outgrew the window", session.SessionActivity{TokensUsed: 250_000}, "250K/1M (25%)"},
		{"tail names a 1M model", session.SessionActivity{TokensUsed: 150_000, Model: "claude-sonnet-5-5"}, "150K/1M (15%)"},
		{"legacy model stays 200K", session.SessionActivity{TokensUsed: 150_000, Model: "claude-sonnet-4-5"}, "150K/200K (75%)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := detailSidebar(t)
			// Loaded at 150K with no model id recorded: the family fallback said 200K.
			sb.SetMetrics("1", &session.SessionMetrics{TokensUsed: 150_000, ContextLimit: 200_000})
			act := tt.act
			sb.SetActivity("1", &act)
			if body := ansi.Strip(sb.View()); !strings.Contains(body, tt.want) {
				t.Errorf("want %q in\n%s", tt.want, body)
			}
		})
	}
}

// Running agents and shells show on every row, not only the selected one.
func TestBackgroundLineOnUnselectedRows(t *testing.T) {
	const width = 45
	sb := NewSidebar(width, 30)
	sb.SetSize(width, 30)
	now := time.Now()
	mk := func(id string) *session.Session {
		return &session.Session{ID: id, DisplayName: strings.Repeat("name"+id, 10), TmuxSession: "alpha",
			TmuxTarget: "alpha:0." + id, Status: session.StatusIdle, LastActivity: now, Summary: "s"}
	}
	sb.SetSessions([]*session.Session{mk("1"), mk("2")})
	sb.SetCursor(1)
	sb.SetBackground(map[string]session.Background{"2": {Agents: 2, Shells: 1, Thinking: true}})

	body := ansi.Strip(sb.View())
	if want := styles.IconAgent + " 2 agents · " + styles.IconShell + " 1 shell · " + styles.IconThinking + " thinking"; !strings.Contains(body, want) {
		t.Errorf("unselected row lacks %q:\n%s", want, body)
	}
	for i, l := range strings.Split(sb.View(), "\n") {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("line %d is %d cells: %q", i, w, ansi.Strip(l))
		}
	}
}

func TestSessionIconIdleWithBackgroundBreathesAsActive(t *testing.T) {
	idle := &session.Session{ID: "a", Status: session.StatusIdle, Attention: attention.Done}
	for frame := 0; frame < 4; frame++ {
		gotFg, gotCh := sessionIconProps(idle, session.Background{Shells: 1}, frame)
		wantFg, wantCh := statusIconProps(session.StatusActive, frame)
		if gotFg != wantFg || gotCh != wantCh {
			t.Errorf("frame %d: idle with background work = (%v, %q), want active (%v, %q)", frame, gotFg, gotCh, wantFg, wantCh)
		}
	}
	if fg, ch := sessionIconProps(idle, session.Background{}, 0); fg != styles.ColorBlue || ch != styles.IconDone {
		t.Errorf("idle without background work = (%v, %q), want done icon", fg, ch)
	}
	waiting := &session.Session{ID: "b", Status: session.StatusWaiting}
	gotFg, gotCh := sessionIconProps(waiting, session.Background{Agents: 1}, 0)
	if wantFg, wantCh := statusIconProps(session.StatusWaiting, 0); gotFg != wantFg || gotCh != wantCh {
		t.Errorf("waiting with background work should stay waiting, got (%v, %q)", gotFg, gotCh)
	}
}
