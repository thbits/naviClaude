package session

import "testing"

func TestResumeShellCommand(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
		id   string
		want string
	}{
		{
			name: "normal cwd and id",
			cwd:  "/home/user/proj",
			id:   "abc-123",
			want: `cd '/home/user/proj' && claude --resume "abc-123"`,
		},
		{
			name: "empty cwd defaults to .",
			cwd:  "",
			id:   "abc-123",
			want: `cd '.' && claude --resume "abc-123"`,
		},
		{
			name: "cwd containing a single quote is escaped safely",
			cwd:  "/home/o'brien/proj",
			id:   "id",
			want: `cd '/home/o'\''brien/proj' && claude --resume "id"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resumeShellCommand(tt.cwd, tt.id)
			if got != tt.want {
				t.Errorf("resumeShellCommand(%q, %q) = %q, want %q", tt.cwd, tt.id, got, tt.want)
			}
		})
	}
}
