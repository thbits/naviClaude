package tmux

import (
	"reflect"
	"testing"
)

func TestParseSessions(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []SessionInfo
	}{
		{
			name: "single line",
			raw:  "work 1719312000",
			want: []SessionInfo{{Name: "work", Activity: 1719312000}},
		},
		{
			name: "multiple lines preserve order",
			raw:  "work 1719312000\nscratch 1719311000\nnaviclaude 1719310000\n",
			want: []SessionInfo{
				{Name: "work", Activity: 1719312000},
				{Name: "scratch", Activity: 1719311000},
				{Name: "naviclaude", Activity: 1719310000},
			},
		},
		{
			name: "session name containing spaces is preserved",
			raw:  "my project 1719312000",
			want: []SessionInfo{{Name: "my project", Activity: 1719312000}},
		},
		{
			name: "empty input",
			raw:  "",
			want: nil,
		},
		{
			name: "blank and whitespace lines skipped",
			raw:  "\n  \nwork 1719312000\n\n",
			want: []SessionInfo{{Name: "work", Activity: 1719312000}},
		},
		{
			name: "lines without activity or with non-numeric activity skipped",
			raw:  "noactivity\nwork 1719312000\nbad notanumber",
			want: []SessionInfo{{Name: "work", Activity: 1719312000}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSessions(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSessions(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}
