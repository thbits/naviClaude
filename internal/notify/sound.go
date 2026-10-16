// Package notify plays notification sounds for session attention events.
package notify

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Built-in sounds come from herdr; see sounds/NOTICE.
//
//go:embed sounds/done.mp3 sounds/request.mp3
var builtin embed.FS

// Sound selects which notification to play.
type Sound string

const (
	Request Sound = "request" // Claude needs input
	Done    Sound = "done"    // a turn finished
)

// DisableEnv silences all playback when set (used by tests and CI).
const DisableEnv = "NAVICLAUDE_DISABLE_SOUND"

// Play plays custom (a file path, "" for the built-in sound, "none" for
// silence) at volume 0-1 and blocks until playback ends. It is a no-op off
// macOS, where afplay is unavailable.
func Play(s Sound, custom string, volume float64) error {
	if os.Getenv(DisableEnv) != "" || runtime.GOOS != "darwin" || strings.EqualFold(custom, "none") {
		return nil
	}
	path := expandHome(custom)
	if path == "" {
		tmp, err := extractBuiltin(s)
		if err != nil {
			return err
		}
		defer os.Remove(tmp)
		path = tmp
	}
	out, err := exec.Command("afplay", "-v", strconv.FormatFloat(volume, 'f', 2, 64), path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("afplay %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func extractBuiltin(s Sound) (string, error) {
	data, err := builtin.ReadFile("sounds/" + string(s) + ".mp3")
	if err != nil {
		return "", fmt.Errorf("unknown sound %q: %w", s, err)
	}
	f, err := os.CreateTemp("", "naviclaude-"+string(s)+"-*.mp3")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
