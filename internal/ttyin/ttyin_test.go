package ttyin

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const wheel = "\x1b[<65;116;36M"

// burst is 255 bytes of reports, so one more byte fills a 256-byte read.
var burst = strings.Repeat(wheel, 19) + "aaaaaaaa"

// chunkFile returns one chunk per Read, like a terminal delivering bursts.
type chunkFile struct{ parts []string }

func (c *chunkFile) Read(p []byte) (int, error) {
	if len(c.parts) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.parts[0])
	if n < len(c.parts[0]) {
		c.parts[0] = c.parts[0][n:]
	} else {
		c.parts = c.parts[1:]
	}
	return n, nil
}
func (c *chunkFile) Write(p []byte) (int, error) { return len(p), nil }
func (c *chunkFile) Close() error                { return nil }
func (c *chunkFile) Fd() uintptr                 { return 0 }
func (c *chunkFile) Name() string                { return "chunks" }

func readAll(t *testing.T, r io.Reader, bufSize int) []string {
	t.Helper()
	var out []string
	buf := make([]byte, bufSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, string(buf[:n]))
		}
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestReaderKeepsSequencesWhole(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  []string
	}{
		{"whole sequence passes", []string{wheel}, []string{wheel}},
		{"split before final byte", []string{"a" + wheel[:12], wheel[12:]}, []string{"a", wheel}},
		{"full read split after ESC", []string{burst + "\x1b", wheel[1:]}, []string{burst, wheel}},
		{"Esc ending a short read", []string{wheel + "\x1b"}, []string{wheel + "\x1b"}},
		{"split across three reads", []string{"x\x1b[<6", "5;11", "6;36M"}, []string{"x", wheel}},
		{"lone Esc keypress", []string{"\x1b"}, []string{"\x1b"}},
		{"lone Alt+[ keypress", []string{"\x1b["}, []string{"\x1b["}},
		{"alt+key is complete", []string{"a\x1bb"}, []string{"a\x1bb"}},
		{"plain text", []string{"hello"}, []string{"hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readAll(t, New(&chunkFile{parts: tt.parts}), 256)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("reads = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReaderReleasesOverlongTail(t *testing.T) {
	long := "a\x1b[" + strings.Repeat("1", maxHeld)
	got := readAll(t, New(&chunkFile{parts: []string{long}}), 256)
	if strings.Join(got, "") != long || len(got) != 1 {
		t.Errorf("reads = %q, want the input in one read", got)
	}
}

// readOnly hides File so Bubble Tea treats the reader as plain custom input.
type readOnly struct{ io.Reader }

type recorder struct{ got *[]string }

func (r recorder) Init() tea.Cmd { return nil }
func (r recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		*r.got = append(*r.got, "key "+msg.String())
	case tea.MouseMsg:
		*r.got = append(*r.got, "mouse "+msg.String())
	}
	return r, nil
}
func (r recorder) View() string { return "" }

// TestBubbleTeaSeesNoKeysFromSplitWheel feeds the splits that leaked
// "<65;116;36" into Claude's prompt through Bubble Tea's own parser.
func TestBubbleTeaSeesNoKeysFromSplitWheel(t *testing.T) {
	full := strings.Repeat(wheel, 19) // 247 bytes: the next report straddles Bubble Tea's 256-byte buffer
	cases := map[string][]string{
		"short read before final byte": {wheel[:12], wheel[12:]},
		"full buffer straddle":         {full + wheel[:9], wheel[9:]},
	}
	for name, parts := range cases {
		t.Run(name, func(t *testing.T) {
			var got []string
			in := readOnly{New(&chunkFile{parts: parts})}
			p := tea.NewProgram(recorder{&got}, tea.WithInput(in), tea.WithOutput(io.Discard))
			go func() { time.Sleep(100 * time.Millisecond); p.Quit() }()
			if _, err := p.Run(); err != nil {
				t.Fatal(err)
			}
			wantMice := strings.Count(strings.Join(parts, ""), wheel)
			for _, g := range got {
				if !strings.HasPrefix(g, "mouse ") {
					t.Fatalf("got non-mouse msg %q; all: %v", g, got)
				}
			}
			if len(got) != wantMice {
				t.Errorf("got %d mouse msgs, want %d: %s", len(got), wantMice, fmt.Sprint(got))
			}
		})
	}
}
