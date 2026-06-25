package session

import "testing"

// TestParseResumeFlagInlineEquals_EdgeCases complements TestParseResumeFlagInlineForm
// in fixes_owned_test.go. It targets fix (1): the --resume=<uuid> inline form must
// split on the FIRST '=' only, must lose to nothing when the value is empty, and
// must keep working when surrounded by unrelated flags.
func TestParseResumeFlagInlineEquals_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		cmdLine string
		want    string
	}{
		{
			name:    "value itself contains an equals sign",
			cmdLine: "claude --resume=a=b=c",
			want:    "a=b=c",
		},
		{
			name:    "inline form is the very first token",
			cmdLine: "--resume=only-token",
			want:    "only-token",
		},
		{
			name:    "inline form preferred when it appears before a space form",
			cmdLine: "claude --resume=first --resume second",
			want:    "first",
		},
		{
			name:    "space form wins when it appears first",
			cmdLine: "claude --resume spaced --extra=ignored",
			want:    "spaced",
		},
		{
			name:    "no resume flag at all",
			cmdLine: "claude --continue --verbose",
			want:    "",
		},
		{
			name:    "resume-like prefix is not matched (must be exactly --resume=)",
			cmdLine: "claude --resumed=nope",
			want:    "",
		},
		{
			name:    "trailing whitespace around inline value is trimmed by Fields",
			cmdLine: "  claude   --resume=padded   ",
			want:    "padded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseResumeFlag(tt.cmdLine); got != tt.want {
				t.Errorf("parseResumeFlag(%q) = %q, want %q", tt.cmdLine, got, tt.want)
			}
		})
	}
}

// TestShellSingleQuote_CombinedMetacharacters complements TestShellSingleQuoteEscaping.
// It targets fix (2): a single path that combines a space, $, and backtick (the
// exact trio called out by the task) must be wrapped in one shell-safe single-quoted
// token that suppresses all expansion, and embedded single quotes must still escape.
func TestShellSingleQuote_CombinedMetacharacters(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "space dollar and backtick together",
			in:   "/tmp/my dir/$USER/`id`",
			want: "'/tmp/my dir/$USER/`id`'",
		},
		{
			name: "all metacharacters plus an embedded single quote",
			in:   "/a b/$x/`y`/o'brien",
			want: `'/a b/$x/` + "`y`" + `/o'\''brien'`,
		},
		{
			name: "subshell and arithmetic expansion are inert",
			in:   "/p/$(rm -rf x)/$((1+1))",
			want: "'/p/$(rm -rf x)/$((1+1))'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellSingleQuote(tt.in)
			if got != tt.want {
				t.Errorf("shellSingleQuote(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// Structural invariant: the result is one single-quoted token, so every
			// run of characters between the escape sequences must be opaque to the
			// shell. The result must always start and end with a single quote.
			if len(got) < 2 || got[0] != '\'' || got[len(got)-1] != '\'' {
				t.Errorf("shellSingleQuote(%q) = %q is not single-quote wrapped", tt.in, got)
			}
		})
	}
}
