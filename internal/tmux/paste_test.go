package tmux

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPasteBufferNameIsUniquePerCall(t *testing.T) {
	a, b := pasteBufferName(), pasteBufferName()
	if a == b {
		t.Fatalf("two calls share buffer %q", a)
	}
	if !strings.Contains(a, "-"+strconv.Itoa(os.Getpid())+"-") {
		t.Errorf("buffer %q does not carry the pid", a)
	}
}

// Each paste lands in its own pane and leaves no buffer behind.
func TestPasteTextOnRealTmux(t *testing.T) {
	tm, _ := privateTmux(t)
	tm("-f", "/dev/null", "new-session", "-d", "-s", "p", "-x", "80", "-y", "24", "cat")
	tm("split-window", "-t", "p:0", "cat")
	c := &Client{}
	if err := c.PasteText("p:0.0", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := c.PasteText("p:0.1", "bravo"); err != nil {
		t.Fatal(err)
	}
	var a, b string
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		a, b = tm("capture-pane", "-p", "-t", "p:0.0"), tm("capture-pane", "-p", "-t", "p:0.1")
		if strings.Contains(a, "alpha") && strings.Contains(b, "bravo") {
			break
		}
	}
	if !strings.Contains(a, "alpha") || strings.Contains(a, "bravo") || !strings.Contains(b, "bravo") {
		t.Errorf("pane 0 = %q, pane 1 = %q", a, b)
	}
	if bufs := tm("list-buffers"); bufs != "" {
		t.Errorf("buffers left behind: %q", bufs)
	}
}
