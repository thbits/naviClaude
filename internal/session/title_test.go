package session

import "testing"

func TestResolveTitle(t *testing.T) {
	tests := []struct {
		name       string
		metaName   string
		nameSource string
		aiTitle    string
		want       string
	}{
		{"derived name yields to aiTitle", "tomhalo-fe", "derived", "Configure Hindsight", "Configure Hindsight"},
		{"non-derived name wins over aiTitle", "my-name", "user", "Configure Hindsight", "my-name"},
		{"derived name with no aiTitle stays", "tomhalo-fe", "derived", "", "tomhalo-fe"},
		{"empty meta with aiTitle uses aiTitle", "", "", "Closed session title", "Closed session title"},
		{"all empty yields empty", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveTitle(tt.metaName, tt.nameSource, tt.aiTitle); got != tt.want {
				t.Errorf("resolveTitle(%q,%q,%q) = %q, want %q",
					tt.metaName, tt.nameSource, tt.aiTitle, got, tt.want)
			}
		})
	}
}
