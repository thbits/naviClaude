package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSemver(t *testing.T) {
	tests := []struct {
		in                        string
		wantMaj, wantMin, wantPat int
		wantPre                   string
		wantOK                    bool
	}{
		{"1.2.3", 1, 2, 3, "", true},
		{"v1.2.3", 1, 2, 3, "", true},
		{"v0.0.1", 0, 0, 1, "", true},
		{"1.2.3-rc1", 1, 2, 3, "rc1", true},
		{"1.2.3-rc.2", 1, 2, 3, "rc.2", true},
		{"1.2.3+build5", 1, 2, 3, "", true},
		{"1.2.3-rc1+build5", 1, 2, 3, "rc1", true},
		{"dev", 0, 0, 0, "", false},
		{"1.2", 0, 0, 0, "", false},
		{"garbage", 0, 0, 0, "", false},
		{"", 0, 0, 0, "", false},
	}
	for _, tt := range tests {
		maj, min, pat, pre, ok := parseSemver(tt.in)
		if ok != tt.wantOK || maj != tt.wantMaj || min != tt.wantMin || pat != tt.wantPat || pre != tt.wantPre {
			t.Errorf("parseSemver(%q) = (%d,%d,%d,%q,%v), want (%d,%d,%d,%q,%v)",
				tt.in, maj, min, pat, pre, ok, tt.wantMaj, tt.wantMin, tt.wantPat, tt.wantPre, tt.wantOK)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int // sign only
	}{
		{"1.3.0", "1.2.3", 1},
		{"1.2.3", "1.3.0", -1},
		{"1.2.3", "1.2.3", 0},
		{"v2.0.0", "1.9.9", 1},
		{"1.2.4", "1.2.3", 1},
		{"garbage", "1.2.3", -1}, // unparseable sorts older
		{"1.2.3", "garbage", 1},
		// Pre-release precedence: a final release is newer than its rc.
		{"1.2.3", "1.2.3-rc1", 1},
		{"1.2.3-rc1", "1.2.3", -1},
		{"1.2.3-rc2", "1.2.3-rc1", 1},
		{"1.2.3-rc1", "1.2.3-rc1", 0},
		{"1.2.3-rc.2", "1.2.3-rc.10", -1}, // numeric identifiers compare numerically
	}
	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		if sign(got) != tt.want {
			t.Errorf("compareSemver(%q,%q) sign = %d, want %d", tt.a, tt.b, sign(got), tt.want)
		}
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}

func TestIsReleaseVersion(t *testing.T) {
	if isReleaseVersion("dev") {
		t.Error("dev should not be a release version")
	}
	if isReleaseVersion("") {
		t.Error("empty should not be a release version")
	}
	if !isReleaseVersion("1.2.3") {
		t.Error("1.2.3 should be a release version")
	}
	if !isReleaseVersion("v1.2.3") {
		t.Error("v1.2.3 should be a release version")
	}
}

// newServer returns an httptest server that responds with the given tag_name
// and wires the env overrides so Check hits it with an isolated cache dir.
func newServer(t *testing.T, tag string, status int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("NAVICLAUDE_UPDATE_URL", srv.URL)
	t.Setenv("NAVICLAUDE_CACHE_DIR", t.TempDir())
}

func TestCheck_UpdateAvailable(t *testing.T) {
	newServer(t, "v1.3.0", http.StatusOK)
	latest, available := Check(context.Background(), "1.2.3")
	if !available || latest != "1.3.0" {
		t.Errorf("Check = (%q,%v), want (\"1.3.0\",true)", latest, available)
	}
}

func TestCheck_UpToDate(t *testing.T) {
	newServer(t, "v1.2.3", http.StatusOK)
	latest, available := Check(context.Background(), "1.2.3")
	if available || latest != "" {
		t.Errorf("Check = (%q,%v), want (\"\",false)", latest, available)
	}
}

func TestCheck_DevBuildSkips(t *testing.T) {
	newServer(t, "v9.9.9", http.StatusOK)
	if latest, available := Check(context.Background(), "dev"); available || latest != "" {
		t.Errorf("dev build Check = (%q,%v), want (\"\",false)", latest, available)
	}
}

func TestCheck_HTTPErrorSilent(t *testing.T) {
	newServer(t, "", http.StatusInternalServerError)
	if latest, available := Check(context.Background(), "1.2.3"); available || latest != "" {
		t.Errorf("error Check = (%q,%v), want (\"\",false)", latest, available)
	}
}

func TestCheck_UsesFreshCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NAVICLAUDE_CACHE_DIR", dir)
	// Seed a fresh cache pointing at a newer version.
	writeSeedCache(t, dir, cacheEntry{CheckedAt: time.Now(), LatestTag: "v2.0.0"})
	// Point the URL at a dead server; a network hit would fail the assertion.
	t.Setenv("NAVICLAUDE_UPDATE_URL", "http://127.0.0.1:0/nope")

	latest, available := Check(context.Background(), "1.2.3")
	if !available || latest != "2.0.0" {
		t.Errorf("cached Check = (%q,%v), want (\"2.0.0\",true)", latest, available)
	}
}

func TestCheck_ExpiredCacheRefetches(t *testing.T) {
	dir := t.TempDir()
	newServer(t, "v1.5.0", http.StatusOK)
	t.Setenv("NAVICLAUDE_CACHE_DIR", dir)
	// Seed a stale cache (older than TTL) with an outdated tag.
	writeSeedCache(t, dir, cacheEntry{CheckedAt: time.Now().Add(-7 * time.Hour), LatestTag: "v1.2.3"})

	latest, available := Check(context.Background(), "1.2.3")
	if !available || latest != "1.5.0" {
		t.Errorf("expired-cache Check = (%q,%v), want (\"1.5.0\",true)", latest, available)
	}
}

// A pre-release build must still be told about its final release.
func TestCheck_PreReleaseSeesFinalRelease(t *testing.T) {
	newServer(t, "v1.2.3", http.StatusOK)
	latest, available := Check(context.Background(), "1.2.3-rc1")
	if !available || latest != "1.2.3" {
		t.Errorf("Check = (%q,%v), want (\"1.2.3\",true)", latest, available)
	}
}

// A failed fetch is cached (with an empty tag) so repeated launches don't each
// re-fire a doomed network request.
func TestCheck_CachesFetchFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NAVICLAUDE_CACHE_DIR", dir)
	t.Setenv("NAVICLAUDE_UPDATE_URL", "http://127.0.0.1:0/nope") // unreachable

	if latest, available := Check(context.Background(), "1.2.3"); available || latest != "" {
		t.Fatalf("Check on failure = (%q,%v), want (\"\",false)", latest, available)
	}

	entry, err := readCache()
	if err != nil {
		t.Fatalf("expected a negative cache entry, readCache err: %v", err)
	}
	if entry.LatestTag != "" {
		t.Errorf("negative cache LatestTag = %q, want empty", entry.LatestTag)
	}
	if entry.CheckedAt.IsZero() {
		t.Error("negative cache CheckedAt should be set")
	}
}

// A cache timestamped in the future (clock skew, restored snapshot) must not be
// treated as perpetually fresh -- the check should refetch.
func TestCheck_FutureCacheTimestampRefetches(t *testing.T) {
	dir := t.TempDir()
	newServer(t, "v1.5.0", http.StatusOK)
	t.Setenv("NAVICLAUDE_CACHE_DIR", dir)
	writeSeedCache(t, dir, cacheEntry{CheckedAt: time.Now().Add(48 * time.Hour), LatestTag: "v1.2.3"})

	latest, available := Check(context.Background(), "1.2.3")
	if !available || latest != "1.5.0" {
		t.Errorf("future-cache Check = (%q,%v), want (\"1.5.0\",true)", latest, available)
	}
}

func writeSeedCache(t *testing.T, dir string, entry cacheEntry) {
	t.Helper()
	data, _ := json.Marshal(entry)
	if err := os.WriteFile(filepath.Join(dir, "update-check.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
