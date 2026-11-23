package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResumeShellCommand(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
		cmd  string
		want string
	}{
		{
			name: "normal cwd and id",
			cwd:  "/home/user/proj",
			cmd:  `claude --resume "abc-123"`,
			want: `cd '/home/user/proj' && claude --resume "abc-123"`,
		},
		{
			name: "empty cwd defaults to .",
			cwd:  "",
			cmd:  `claude --resume "abc-123"`,
			want: `cd '.' && claude --resume "abc-123"`,
		},
		{
			name: "cwd containing a single quote is escaped safely",
			cwd:  "/home/o'brien/proj",
			cmd:  `claude --resume "id"`,
			want: `cd '/home/o'\''brien/proj' && claude --resume "id"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resumeShellCommand(tt.cwd, tt.cmd)
			if got != tt.want {
				t.Errorf("resumeShellCommand(%q, %q) = %q, want %q", tt.cwd, tt.cmd, got, tt.want)
			}
		})
	}
}

func TestNameWindow(t *testing.T) {
	tests := []struct {
		name, format, project, harness, want string
	}{
		{"project name wins", "", "proj", "claude", "proj"},
		{"falls back to harness", "{project}", "", "claude", "claude"},
		{"root dir falls back to harness", "{project}", "/", "claude", "claude"},
		{"unstamped falls back to generic", "{project}", "", "", "session"},
		{"template expands both", "{harness}: {project}", "proj", "codex", "codex: proj"},
		{"literal name", "agent", "proj", "claude", "agent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager(nil, tt.format)
			if got := m.nameWindow(tt.project, tt.harness); got != tt.want {
				t.Errorf("nameWindow(%q, %q) = %q, want %q", tt.project, tt.harness, got, tt.want)
			}
		})
	}
}

// The window command survives a cwd with quotes and $ intact, then hands off to $SHELL.
func TestResumeWindowCommandQuoting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), `it's $HOME`)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", resumeWindowCommand(resumeShellCommand(dir, "pwd")))
	cmd.Env = append(os.Environ(), "SHELL=/bin/echo")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	want := dir + "\n-l\n" // pwd, then the fallback shell (echo) gets -l
	if got, _ := filepath.EvalSymlinks(strings.SplitN(string(out), "\n", 2)[0]); got != mustEvalDir(t, dir) || !strings.HasSuffix(string(out), "\n-l\n") {
		t.Errorf("output %q, want %q", out, want)
	}
}

func mustEvalDir(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
