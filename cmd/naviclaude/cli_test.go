package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thbits/naviClaude/internal/session"
	"github.com/thbits/naviClaude/skills"
)

// fakeClock advances only when Sleep is called.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time        { return c.t }
func (c *fakeClock) Sleep(d time.Duration) { c.t = c.t.Add(d) }

// quiet runs f with stdout and stderr discarded.
func quiet(f func() int) int {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return f()
	}
	defer devnull.Close()
	out, errOut := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, devnull
	defer func() { os.Stdout, os.Stderr = out, errOut }()
	return f()
}

// scripted returns frames[i] on the i-th call, repeating the last one.
func scripted(frames ...[]agentInfo) (agentSource, *int) {
	n := 0
	return func() ([]agentInfo, error) {
		i := min(n, len(frames)-1)
		n++
		return frames[i], nil
	}, &n
}

func ag(pane, status string) agentInfo {
	return agentInfo{ID: "id-" + pane, PaneID: pane, Name: "n" + pane, Target: "s:1." + pane[1:], Status: status}
}

func TestParseInterleaved(t *testing.T) {
	tests := []struct {
		args      []string
		pos       []string
		until     string
		wait      bool
		wantError bool
	}{
		{args: []string{"%1", "--until", "idle"}, pos: []string{"%1"}, until: "idle"},
		{args: []string{"--until=idle", "%1"}, pos: []string{"%1"}, until: "idle"},
		{args: []string{"%1", "fix", "it", "--wait"}, pos: []string{"%1", "fix", "it"}, wait: true},
		{args: []string{"%1", "--wait", "--", "--not-a-flag", "x"}, pos: []string{"%1", "--not-a-flag", "x"}, wait: true},
		{args: []string{"%1", "--bogus"}, wantError: true},
	}
	for _, tt := range tests {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		until := fs.String("until", "", "")
		wait := fs.Bool("wait", false, "")
		pos, err := parseInterleaved(fs, tt.args)
		if tt.wantError {
			if err == nil {
				t.Errorf("%v: want error", tt.args)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(pos, tt.pos) || *until != tt.until || *wait != tt.wait {
			t.Errorf("%v: pos=%v until=%q wait=%v err=%v", tt.args, pos, *until, *wait, err)
		}
	}
}

func TestResolveTarget(t *testing.T) {
	list := []agentInfo{
		{ID: "3cb8566e-aaaa", PaneID: "%185", Target: "default:8.1", Name: "grafana"},
		{ID: "f487b4a2-bbbb", PaneID: "%187", Target: "default:9.1", Name: "tomhalo-92"},
		{ID: "b023b1d3-cccc", PaneID: "%144", Target: "default:4.1", Name: "tomhalo-92"},
		{ID: "b023b1ff-dddd", PaneID: "%150", Target: "work:1.0", Name: "API"},
	}
	ok := map[string]string{
		"3cb8566e-aaaa": "%185", // exact id
		"%187":          "%187", // pane id
		"default:4.1":   "%144", // tmux target
		"grafana":       "%185", // name
		"api":           "%150", // case-insensitive name
		"3cb856":        "%185", // id prefix
		"b023b1d":       "%144",
	}
	for q, want := range ok {
		got, err := resolveTarget(list, q)
		if err != nil || got.PaneID != want {
			t.Errorf("resolve %q = %q, %v; want %q", q, got.PaneID, err, want)
		}
	}
	for _, q := range []string{"tomhalo-92", "b023b1"} {
		_, err := resolveTarget(list, q)
		if err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "%144") {
			t.Errorf("resolve %q: want ambiguity listing candidates, got %v", q, err)
		}
	}
	// Short or non-hex prefixes are too likely to hit a session by accident.
	for _, q := range []string{"nope", "3cb8", "3", "b023b", "3cb85g"} {
		if _, err := resolveTarget(list, q); !errors.Is(err, errNoMatch) {
			t.Errorf("target %q: got %v, want no match", q, err)
		}
	}
	if _, err := resolveTarget(list, ""); !errors.Is(err, errNoMatch) {
		t.Errorf("empty target: got %v", err)
	}
}

func TestDeriveStatus(t *testing.T) {
	tests := []struct {
		name   string
		s      session.Session
		act    *session.SessionActivity
		status string
		reason string
	}{
		{"idle", session.Session{Status: session.StatusIdle}, nil, stIdle, ""},
		{"done marker", session.Session{Status: session.StatusIdle, Attention: "done"}, nil, stDone, ""},
		{"working", session.Session{Status: session.StatusActive}, nil, stWorking, ""},
		{"background keeps working", session.Session{Status: session.StatusIdle, Attention: "done"},
			&session.SessionActivity{Shells: 1}, stWorking, ""},
		{"approval", session.Session{Status: session.StatusWaiting},
			&session.SessionActivity{Tool: "Edit", Target: "main.go"}, stWaiting, "needs approval: Edit main.go"},
		{"question", session.Session{Status: session.StatusWaiting},
			&session.SessionActivity{Tool: "AskUserQuestion"}, stWaiting, "question"},
		{"plan", session.Session{Status: session.StatusWaiting},
			&session.SessionActivity{Tool: "ExitPlanMode"}, stWaiting, "plan approval"},
		{"native reason", session.Session{Status: session.StatusWaiting, WaitingFor: "permission prompt"}, nil,
			stWaiting, "permission prompt"},
		{"no reason", session.Session{Status: session.StatusWaiting}, nil, stWaiting, "needs input"},
	}
	for _, tt := range tests {
		st, why := deriveStatus(&tt.s, tt.act)
		if st != tt.status || why != tt.reason {
			t.Errorf("%s: got %q/%q, want %q/%q", tt.name, st, why, tt.status, tt.reason)
		}
	}
}

func TestToAgentInfoNameAndBackground(t *testing.T) {
	s := &session.Session{ID: "x", ProjectName: "proj", Status: session.StatusIdle, Model: "opus",
		LastActivity: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	a := toAgentInfo(s, &session.SessionActivity{Agents: 2, Shells: 1, Model: "claude-opus-5-5"})
	if a.Name != "proj" || a.Status != stWorking || a.Background != "2 agents, 1 shell" ||
		a.Model != "claude-opus-5-5" || a.LastActivity != "2026-01-02T03:04:05Z" {
		t.Errorf("got %+v", a)
	}
	if got := a.statusLabel(); got != "working (2 agents, 1 shell)" {
		t.Errorf("label %q", got)
	}
	s.Slug, s.DisplayName = "slug", ""
	if sessionName(s) != "slug" {
		t.Error("slug should beat project")
	}
	s.DisplayName = "renamed"
	if sessionName(s) != "renamed" {
		t.Error("display name should win")
	}
}

func TestWriteAgentsJSONShape(t *testing.T) {
	var buf bytes.Buffer
	if err := writeAgentsJSON(&buf, nil); err != nil || strings.TrimSpace(buf.String()) != "[]" {
		t.Fatalf("empty list: %q %v", buf.String(), err)
	}
	buf.Reset()
	if err := writeAgentsJSON(&buf, []agentInfo{{ID: "a", stamp: 42}}); err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range out[0] {
		keys = append(keys, k)
	}
	want := []string{"background", "branch", "cwd", "id", "last_activity", "model", "name", "pane_id", "pid",
		"project", "reason", "self", "status", "subprocess", "target", "tmux_session"}
	if len(keys) != len(want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	for _, k := range want {
		if _, ok := out[0][k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
}

func TestWriteAgentsTable(t *testing.T) {
	var buf bytes.Buffer
	a := ag("%3", stWaiting)
	a.Reason, a.Self = "question", true
	if err := writeAgentsTable(&buf, []agentInfo{a}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "%3*") || !strings.Contains(out, "waiting (question)") {
		t.Errorf("table:\n%s", out)
	}
}

func TestWaitIdleIsDebounced(t *testing.T) {
	src, calls := scripted(
		[]agentInfo{ag("%1", stWorking)},
		[]agentInfo{ag("%1", stIdle)}, // a gap between tool calls
		[]agentInfo{ag("%1", stWorking)},
		[]agentInfo{ag("%1", stIdle)},
		[]agentInfo{ag("%1", stDone)},
	)
	w := waiter{src: src, clk: &fakeClock{}, interval: time.Second}
	got, out, err := w.waitFor(ag("%1", stWorking), untilConds["idle"], time.Minute)
	if err != nil || out != waitMatched || got.Status != stDone || *calls != 5 {
		t.Errorf("got %v/%v/%v after %d polls", got.Status, out, err, *calls)
	}
}

func TestWaitWaitingMatchesAtOnce(t *testing.T) {
	src, calls := scripted([]agentInfo{ag("%1", stWorking)}, []agentInfo{ag("%1", stWaiting)})
	w := waiter{src: src, clk: &fakeClock{}, interval: time.Second}
	_, out, _ := w.waitFor(ag("%1", stWorking), untilConds["any-attention"], time.Minute)
	if out != waitMatched || *calls != 2 {
		t.Errorf("out=%v calls=%d", out, *calls)
	}
}

func TestWaitTimeout(t *testing.T) {
	src, _ := scripted([]agentInfo{ag("%1", stWorking)})
	clk := &fakeClock{t: time.Unix(0, 0)}
	w := waiter{src: src, clk: clk, interval: time.Second}
	got, out, _ := w.waitFor(ag("%1", stWorking), untilConds["idle"], 5*time.Second)
	if out != waitTimedOut || got.Status != stWorking || clk.t.Sub(time.Unix(0, 0)) != 5*time.Second {
		t.Errorf("out=%v status=%s elapsed=%s", out, got.Status, clk.t.Sub(time.Unix(0, 0)))
	}
}

func TestWaitGoneAndPaneIdentity(t *testing.T) {
	cleared := ag("%1", stIdle)
	cleared.ID = "new-id-after-clear"
	src, _ := scripted([]agentInfo{ag("%1", stWorking)}, []agentInfo{cleared})
	w := waiter{src: src, clk: &fakeClock{}, interval: time.Second}
	if _, out, _ := w.waitFor(ag("%1", stWorking), untilConds["idle"], time.Minute); out != waitMatched {
		t.Errorf("pane should survive an id change, got %v", out)
	}
	src, _ = scripted([]agentInfo{ag("%1", stWorking)}, []agentInfo{ag("%2", stIdle)})
	w.src = src
	if _, out, _ := w.waitFor(ag("%1", stWorking), untilConds["idle"], time.Minute); out != waitGone {
		t.Errorf("want gone, got %v", out)
	}
}

// flaky fails the calls listed in fail (1-based), else returns list.
func flaky(list []agentInfo, fail ...int) (agentSource, *int) {
	n := 0
	return func() ([]agentInfo, error) {
		n++
		for _, f := range fail {
			if n == f {
				return nil, errors.New("tmux busy")
			}
		}
		return list, nil
	}, &n
}

func TestWaitToleratesTransientErrors(t *testing.T) {
	idle := []agentInfo{ag("%1", stIdle)}
	src, calls := flaky(idle, 1, 2, 4)
	w := waiter{src: src, clk: &fakeClock{}, interval: time.Second}
	if _, out, err := w.waitFor(ag("%1", stWorking), untilConds["idle"], time.Minute); out != waitMatched || err != nil || *calls != 5 {
		t.Errorf("out=%v err=%v after %d polls; want matched after 5", out, err, *calls)
	}
	src, calls = flaky(idle, 1, 2, 3)
	w.src = src
	if _, out, err := w.waitFor(ag("%1", stWorking), untilConds["idle"], time.Minute); out != waitGone || err == nil || *calls != 3 {
		t.Errorf("out=%v err=%v after %d polls; want gone after 3 errors", out, err, *calls)
	}
	// Errors still respect the deadline.
	src, _ = flaky(idle, 1, 2)
	clk := &fakeClock{t: time.Unix(0, 0)}
	w = waiter{src: src, clk: clk, interval: time.Second}
	if _, out, _ := w.waitFor(ag("%1", stWorking), untilConds["idle"], time.Second); out != waitTimedOut {
		t.Errorf("out=%v, want timeout while erroring", out)
	}
}

func TestWaitMainArgs(t *testing.T) {
	list := []agentInfo{ag("%1", stIdle)}
	newSrc := func() agentSource { s, _ := scripted(list); return s }
	cases := map[string]struct {
		args []string
		code int
	}{
		"missing until": {[]string{"%1"}, exitUsage},
		"bad until":     {[]string{"%1", "--until", "sleepy"}, exitUsage},
		"no target":     {[]string{"--until", "idle"}, exitUsage},
		"unknown":       {[]string{"%9", "--until", "idle"}, exitUsage},
		"bad interval":  {[]string{"%1", "--until", "idle", "--interval", "1ms"}, exitUsage},
		"reached":       {[]string{"%1", "--until", "idle"}, exitMatched},
		"timeout":       {[]string{"%1", "--until", "working", "--timeout", "3s"}, exitTimeout},
	}
	for name, c := range cases {
		if got := quiet(func() int { return waitMain(c.args, newSrc, &fakeClock{}) }); got != c.code {
			t.Errorf("%s: exit %d, want %d", name, got, c.code)
		}
	}
}

// fakeTyper records what send typed.
type fakeTyper struct{ calls []string }

func (f *fakeTyper) PasteText(target, text string) error {
	f.calls = append(f.calls, "paste "+target+" "+text)
	return nil
}

func (f *fakeTyper) SendKeys(target, keys string) error {
	f.calls = append(f.calls, "keys "+target+" "+keys)
	return nil
}

func TestSendTypesAndSubmits(t *testing.T) {
	ft := &fakeTyper{}
	src, _ := scripted([]agentInfo{ag("%1", stIdle)})
	deps := func() sendDeps { return sendDeps{src: src, typer: ft, clk: &fakeClock{}, stdin: strings.NewReader("")} }
	if code := quiet(func() int { return sendMain([]string{"%1", "fix", "the", "bug"}, deps) }); code != exitMatched {
		t.Fatalf("exit %d", code)
	}
	want := []string{"paste %1 fix the bug", "keys %1 Enter"}
	if !reflect.DeepEqual(ft.calls, want) {
		t.Errorf("calls = %v", ft.calls)
	}
}

func TestSendStdin(t *testing.T) {
	ft := &fakeTyper{}
	src, _ := scripted([]agentInfo{ag("%1", stIdle)})
	deps := func() sendDeps {
		return sendDeps{src: src, typer: ft, clk: &fakeClock{}, stdin: strings.NewReader("line one\nline two\n")}
	}
	if code := quiet(func() int { return sendMain([]string{"%1", "-"}, deps) }); code != exitMatched {
		t.Fatalf("exit %d", code)
	}
	if ft.calls[0] != "paste %1 line one\nline two" {
		t.Errorf("paste = %q", ft.calls[0])
	}
}

func TestSendRefusals(t *testing.T) {
	waiting := ag("%1", stWaiting)
	waiting.Reason = "needs approval: Bash"
	self := ag("%2", stIdle)
	self.Self = true
	sub := ag("%3", stIdle)
	sub.Subprocess = true
	for _, a := range []agentInfo{waiting, self, sub} {
		ft := &fakeTyper{}
		src, _ := scripted([]agentInfo{a})
		deps := func() sendDeps { return sendDeps{src: src, typer: ft, clk: &fakeClock{}} }
		if code := quiet(func() int { return sendMain([]string{a.PaneID, "yes"}, deps) }); code != exitUsage || len(ft.calls) != 0 {
			t.Errorf("%s: exit %d calls %v; want refusal", a.PaneID, code, ft.calls)
		}
		if code := quiet(func() int { return sendMain([]string{a.PaneID, "yes", "--force"}, deps) }); code != exitMatched || len(ft.calls) != 2 {
			t.Errorf("%s --force: exit %d calls %v", a.PaneID, code, ft.calls)
		}
	}
}

func TestSendWaitSkipsStaleIdle(t *testing.T) {
	before := ag("%1", stIdle)
	before.stamp = 1
	// Claude hasn't reacted yet for two polls, then works, then finishes.
	src, calls := scripted(
		[]agentInfo{before}, // resolve
		[]agentInfo{before},
		[]agentInfo{before},
		[]agentInfo{ag("%1", stWorking)},
		[]agentInfo{ag("%1", stWorking)},
		[]agentInfo{ag("%1", stIdle)},
		[]agentInfo{ag("%1", stIdle)},
	)
	deps := func() sendDeps { return sendDeps{src: src, typer: &fakeTyper{}, clk: &fakeClock{}} }
	if code := quiet(func() int { return sendMain([]string{"%1", "go", "--wait"}, deps) }); code != exitMatched || *calls != 7 {
		t.Errorf("exit %d after %d polls", code, *calls)
	}
}

func TestSendWaitFastTurnViaStamp(t *testing.T) {
	before := ag("%1", stIdle)
	before.stamp = 1
	after := ag("%1", stIdle)
	after.stamp = 2 // the turn started and ended between polls
	src, calls := scripted([]agentInfo{before}, []agentInfo{after}, []agentInfo{after})
	deps := func() sendDeps { return sendDeps{src: src, typer: &fakeTyper{}, clk: &fakeClock{}} }
	// Polls: resolve, the pre-Enter re-check, turn start, then two settled idles.
	if code := quiet(func() int { return sendMain([]string{"%1", "go", "--wait"}, deps) }); code != exitMatched || *calls != 5 {
		t.Errorf("exit %d after %d polls", code, *calls)
	}
}

func TestSendWaitTimeout(t *testing.T) {
	src, _ := scripted([]agentInfo{ag("%1", stIdle)}, []agentInfo{ag("%1", stWorking)})
	deps := func() sendDeps { return sendDeps{src: src, typer: &fakeTyper{}, clk: &fakeClock{}} }
	if code := quiet(func() int { return sendMain([]string{"%1", "go", "--wait", "--timeout", "10s"}, deps) }); code != exitTimeout {
		t.Errorf("exit %d, want timeout", code)
	}
}

// A dialog that opens between the paste and Enter must not be answered by Enter.
func TestSendRechecksWaitingBeforeEnter(t *testing.T) {
	ft := &fakeTyper{}
	src, _ := scripted([]agentInfo{ag("%1", stIdle)}, []agentInfo{ag("%1", stWaiting)})
	deps := func() sendDeps { return sendDeps{src: src, typer: ft, clk: &fakeClock{}} }
	if code := quiet(func() int { return sendMain([]string{"%1", "go"}, deps) }); code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
	if want := []string{"paste %1 go"}; !reflect.DeepEqual(ft.calls, want) {
		t.Errorf("calls = %v, want only the paste", ft.calls)
	}

	ft = &fakeTyper{}
	src, _ = scripted([]agentInfo{ag("%1", stIdle)}, []agentInfo{ag("%1", stWaiting)})
	if code := quiet(func() int { return sendMain([]string{"%1", "go", "--force"}, deps) }); code != exitMatched || len(ft.calls) != 2 {
		t.Errorf("--force: exit %d calls %v", code, ft.calls)
	}

	// A failed re-read is not proof it is safe to press Enter.
	ft = &fakeTyper{}
	n := 0
	src = func() ([]agentInfo, error) {
		if n++; n > 1 {
			return nil, errors.New("tmux gone")
		}
		return []agentInfo{ag("%1", stIdle)}, nil
	}
	if code := quiet(func() int { return sendMain([]string{"%1", "go"}, deps) }); code != exitUsage || len(ft.calls) != 1 {
		t.Errorf("re-read error: exit %d calls %v", code, ft.calls)
	}
}

// A prompt that never starts a turn exits 3 right after the start window.
func TestSendWaitNoStart(t *testing.T) {
	before := ag("%1", stIdle)
	before.stamp = 1
	src, _ := scripted([]agentInfo{before})
	clk := &fakeClock{t: time.Unix(0, 0)}
	deps := func() sendDeps { return sendDeps{src: src, typer: &fakeTyper{}, clk: clk} }
	if code := quiet(func() int { return sendMain([]string{"%1", "go", "--wait"}, deps) }); code != exitNoStart {
		t.Errorf("exit %d, want %d", code, exitNoStart)
	}
	if el := clk.t.Sub(time.Unix(0, 0)); el > turnStartWindow+time.Second {
		t.Errorf("kept waiting %s after the start window", el)
	}
	// A --timeout shorter than the window is still a plain timeout.
	if code := quiet(func() int { return sendMain([]string{"%1", "go", "--wait", "--timeout", "2s"}, deps) }); code != exitTimeout {
		t.Errorf("short --timeout: exit %d, want %d", code, exitTimeout)
	}
}

func TestSendArgs(t *testing.T) {
	deps := func() sendDeps { t.Fatal("deps used on bad args"); return sendDeps{} }
	for _, args := range [][]string{{"%1"}, {}, {"%1", "x", "--timeout", "-1s"}} {
		if code := quiet(func() int { return sendMain(args, deps) }); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

func TestTurnStamp(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "none.jsonl")
	if a, b := turnStamp(7, missing), turnStamp(7, missing); a != b || a != 7 {
		t.Errorf("missing transcript must be stable: %d %d", a, b)
	}
	f := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := turnStamp(7, f)
	if err := os.Chtimes(f, time.Now(), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if turnStamp(7, f) == before || turnStamp(8, f) == turnStamp(7, f) {
		t.Error("stamp must move with the transcript and the status time")
	}
}

func TestSkillEmbedded(t *testing.T) {
	if !strings.HasPrefix(skills.NaviClaude, "---\nname: naviclaude\ndescription: ") {
		t.Errorf("skill frontmatter missing: %.60q", skills.NaviClaude)
	}
}
