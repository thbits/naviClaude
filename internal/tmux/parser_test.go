package tmux

import (
	"testing"
)

func TestParsePanes(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantCount int
		wantFirst PaneInfo
	}{
		{
			name:      "single valid line",
			raw:       "main:0.0\x1fzsh\x1f12345\x1f/home/user/project",
			wantCount: 1,
			wantFirst: PaneInfo{
				SessionName:    "main",
				WindowIndex:    0,
				PaneIndex:      0,
				Target:         "main:0.0",
				CurrentCommand: "zsh",
				PID:            12345,
				CurrentPath:    "/home/user/project",
			},
		},
		{
			name: "multiple valid lines",
			raw: "main:0.0\x1fzsh\x1f100\x1f/home/user\n" +
				"dev:1.2\x1fvim\x1f200\x1f/tmp/work\n",
			wantCount: 2,
			wantFirst: PaneInfo{
				SessionName:    "main",
				WindowIndex:    0,
				PaneIndex:      0,
				Target:         "main:0.0",
				CurrentCommand: "zsh",
				PID:            100,
				CurrentPath:    "/home/user",
			},
		},
		{
			name:      "empty input",
			raw:       "",
			wantCount: 0,
		},
		{
			name:      "only whitespace and empty lines",
			raw:       "\n  \n\n  \n",
			wantCount: 0,
		},
		{
			name:      "malformed lines skipped silently",
			raw:       "not-a-valid-line\nmain:0.0\x1fzsh\x1f100\x1f/home\nbadline",
			wantCount: 1,
			wantFirst: PaneInfo{
				SessionName:    "main",
				WindowIndex:    0,
				PaneIndex:      0,
				Target:         "main:0.0",
				CurrentCommand: "zsh",
				PID:            100,
				CurrentPath:    "/home",
			},
		},
		{
			name:      "path with spaces preserved",
			raw:       "work:3.1\x1fclaude\x1f999\x1f/Users/tom/my project/src dir",
			wantCount: 1,
			wantFirst: PaneInfo{
				SessionName:    "work",
				WindowIndex:    3,
				PaneIndex:      1,
				Target:         "work:3.1",
				CurrentCommand: "claude",
				PID:            999,
				CurrentPath:    "/Users/tom/my project/src dir",
			},
		},
		{
			// Regression: tmux session names may contain spaces. The old
			// space-delimited format dropped these entirely (the name's first
			// space shifted every field, so the target failed to parse).
			name:      "session name with spaces preserved",
			raw:       "navi space test:1.1\x1fzsh\x1f86830\x1f/Users/tom/proj",
			wantCount: 1,
			wantFirst: PaneInfo{
				SessionName:    "navi space test",
				WindowIndex:    1,
				PaneIndex:      1,
				Target:         "navi space test:1.1",
				CurrentCommand: "zsh",
				PID:            86830,
				CurrentPath:    "/Users/tom/proj",
			},
		},
		{
			// Both name and path containing spaces, simultaneously.
			name:      "spaces in both session name and path",
			raw:       "my cool session:2.3\x1fclaude\x1f4242\x1f/Users/tom/my project",
			wantCount: 1,
			wantFirst: PaneInfo{
				SessionName:    "my cool session",
				WindowIndex:    2,
				PaneIndex:      3,
				Target:         "my cool session:2.3",
				CurrentCommand: "claude",
				PID:            4242,
				CurrentPath:    "/Users/tom/my project",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panes, err := ParsePanes(tt.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(panes) != tt.wantCount {
				t.Fatalf("got %d panes, want %d", len(panes), tt.wantCount)
			}
			if tt.wantCount > 0 && panes[0] != tt.wantFirst {
				t.Errorf("first pane = %+v, want %+v", panes[0], tt.wantFirst)
			}
		})
	}
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		wantSess  string
		wantWin   int
		wantPane  int
		wantError bool
	}{
		{
			name:     "simple target",
			target:   "main:0.0",
			wantSess: "main",
			wantWin:  0,
			wantPane: 0,
		},
		{
			name:     "multi-digit indices",
			target:   "dev:12.34",
			wantSess: "dev",
			wantWin:  12,
			wantPane: 34,
		},
		{
			name:     "session name with hyphens",
			target:   "my-session:5.3",
			wantSess: "my-session",
			wantWin:  5,
			wantPane: 3,
		},
		{
			name:      "missing colon",
			target:    "noseparator",
			wantError: true,
		},
		{
			name:      "missing dot",
			target:    "sess:123",
			wantError: true,
		},
		{
			name:      "non-numeric window index",
			target:    "sess:abc.0",
			wantError: true,
		},
		{
			name:      "non-numeric pane index",
			target:    "sess:0.xyz",
			wantError: true,
		},
		{
			name:      "empty target",
			target:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess, win, pane, err := parseTarget(tt.target)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sess != tt.wantSess {
				t.Errorf("session = %q, want %q", sess, tt.wantSess)
			}
			if win != tt.wantWin {
				t.Errorf("window = %d, want %d", win, tt.wantWin)
			}
			if pane != tt.wantPane {
				t.Errorf("pane = %d, want %d", pane, tt.wantPane)
			}
		})
	}
}

func TestParsePaneLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantError bool
	}{
		{
			name:      "too few fields",
			line:      "main:0.0\x1fzsh\x1f100",
			wantError: true,
		},
		{
			name:      "invalid pid",
			line:      "main:0.0\x1fzsh\x1fnotapid\x1f/home",
			wantError: true,
		},
		{
			name:      "invalid target format",
			line:      "badtarget\x1fzsh\x1f100\x1f/home",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePaneLine(tt.line)
			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseCursorInfo(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		want      CursorInfo
		wantError bool
	}{
		{
			name: "visible cursor",
			raw:  "2 64 68 1\n",
			want: CursorInfo{X: 2, Y: 64, PaneHeight: 68, Visible: true},
		},
		{
			name: "hidden cursor",
			raw:  "0 0 24 0",
			want: CursorInfo{X: 0, Y: 0, PaneHeight: 24, Visible: false},
		},
		{
			name:      "too few fields",
			raw:       "2 64 68",
			wantError: true,
		},
		{
			name:      "non-numeric field",
			raw:       "x 64 68 1",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCursorInfo(tt.raw)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseCursorInfo(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}
