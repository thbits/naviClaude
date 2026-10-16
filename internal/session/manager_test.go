package session

import "testing"

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

func TestResumeWindowName(t *testing.T) {
	tests := []struct {
		name string
		s    Session
		want string
	}{
		{"project name wins", Session{ProjectName: "proj", Harness: "claude"}, "proj"},
		{"falls back to harness", Session{Harness: "claude"}, "claude"},
		{"unstamped falls back to generic", Session{}, "session"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resumeWindowName(&tt.s); got != tt.want {
				t.Errorf("resumeWindowName = %q, want %q", got, tt.want)
			}
		})
	}
}
