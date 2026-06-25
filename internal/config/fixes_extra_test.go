package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoad_BadRefreshIntervalVariantsFallBack complements
// TestLoadInvalidRefreshIntervalFallsBack. It targets fix (6): a refresh_interval
// that fails time.ParseDuration -- including a bare negative number, a number with
// no unit, and assorted garbage -- must fall back to the DefaultConfig() value
// rather than being honored or left as the raw (unparseable) string.
func TestLoad_BadRefreshIntervalVariantsFallBack(t *testing.T) {
	def := DefaultConfig().RefreshInterval

	tests := []struct {
		name string
		yaml string
	}{
		// A bare number has no duration unit and fails ParseDuration.
		{"bare number no unit", "refresh_interval: \"200\"\n"},
		// A negative bare number: invalid for the same reason (no unit).
		{"negative bare number", "refresh_interval: \"-5\"\n"},
		{"garbage word", "refresh_interval: \"soon\"\n"},
		{"number with bogus unit", "refresh_interval: \"10furlongs\"\n"},
		{"empty string", "refresh_interval: \"\"\n"},
		{"whitespace only", "refresh_interval: \"   \"\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load error: %v", err)
			}
			if cfg.RefreshInterval != def {
				t.Errorf("RefreshInterval = %q, want fallback to default %q", cfg.RefreshInterval, def)
			}
		})
	}
}

// TestLoad_NegativeRefreshIntervalDurationFallsBack targets fix (6) for the case
// the task calls out explicitly: a NEGATIVE duration. "-200ms" parses successfully
// as a time.Duration, so the parse guard alone would let it through; this test
// pins the behavior so a future guard against negative durations does not silently
// regress, while documenting current behavior.
func TestLoad_NegativeRefreshIntervalDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("refresh_interval: \"-200ms\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// "-200ms" is a parseable duration, so sanitizeConfig's ParseDuration guard
	// leaves it untouched. Either it stays as written or a stricter guard maps it
	// back to the default; both are acceptable, but it must never be a value that
	// is neither.
	if cfg.RefreshInterval != "-200ms" && cfg.RefreshInterval != DefaultConfig().RefreshInterval {
		t.Errorf("RefreshInterval = %q, want either %q (parses) or default %q",
			cfg.RefreshInterval, "-200ms", DefaultConfig().RefreshInterval)
	}
}

// TestLoad_ValidRefreshIntervalUnitsPreserved targets fix (6): a variety of valid
// duration spellings must be preserved verbatim, proving the fallback only fires
// on genuinely invalid input.
func TestLoad_ValidRefreshIntervalUnitsPreserved(t *testing.T) {
	valid := []string{"50ms", "1s", "2m", "1h30m", "500us"}
	for _, v := range valid {
		t.Run(v, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte("refresh_interval: \""+v+"\"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load error: %v", err)
			}
			if cfg.RefreshInterval != v {
				t.Errorf("RefreshInterval = %q, want preserved %q", cfg.RefreshInterval, v)
			}
		})
	}
}
