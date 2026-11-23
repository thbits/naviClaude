package preview

import (
	"io"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// chunkReader returns one chunk per Read, like separate terminal writes, then blocks.
type chunkReader struct{ chunks []string }

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.chunks) == 0 {
		time.Sleep(time.Hour)
		return 0, io.EOF
	}
	n := copy(p, c.chunks[0])
	c.chunks = c.chunks[1:]
	return n, nil
}

type keyRecorder struct{ got *[]string }

func (r keyRecorder) Init() tea.Cmd { return nil }
func (r keyRecorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		*r.got = append(*r.got, translateKey(k))
	}
	return r, nil
}
func (r keyRecorder) View() string { return "" }

// Esc must reach the pane promptly and byte-for-byte: tmux sends "M-x" as ESC x,
// so a merged Esc+key still delivers the bytes that were typed.
func TestEscForwardedAsTyped(t *testing.T) {
	cases := map[string]struct {
		reads []string
		want  []string
	}{
		"lone Esc":               {[]string{"\x1b"}, []string{"Escape"}},
		"Esc then key":           {[]string{"\x1b", "x"}, []string{"Escape", "x"}},
		"double Esc in one read": {[]string{"\x1b\x1b"}, []string{"M-Escape"}},
		"Esc+key in one read":    {[]string{"\x1bx"}, []string{"M-x"}},
		"Alt+[ alone":            {[]string{"\x1b["}, []string{"M-["}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got []string
			p := tea.NewProgram(keyRecorder{&got}, tea.WithInput(&chunkReader{chunks: tc.reads}), tea.WithOutput(io.Discard))
			go func() { time.Sleep(100 * time.Millisecond); p.Quit() }()
			if _, err := p.Run(); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("forwarded %q, want %q", got, tc.want)
			}
		})
	}
}
