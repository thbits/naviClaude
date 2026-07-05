// Package update checks whether a newer naviClaude release is available on
// GitHub. It is deliberately self-contained and UI-agnostic: it performs a
// single cached HTTP query against the GitHub Releases API and reports the
// latest version when it is newer than the running build.
//
// Every failure path (no network, timeout, non-200, unparseable JSON or
// version, or a "dev" build) is silent: Check returns ("", false) so callers
// can simply show nothing.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// latestReleaseURL is the GitHub API endpoint for the newest published release.
const latestReleaseURL = "https://api.github.com/repos/thbits/naviClaude/releases/latest"

// cacheTTL is how long a check result is reused before hitting the network.
const cacheTTL = 6 * time.Hour

// httpTimeout bounds the network call so startup is never blocked for long.
const httpTimeout = 3 * time.Second

// cacheEntry is the on-disk cache format.
type cacheEntry struct {
	CheckedAt time.Time `json:"checked_at"`
	LatestTag string    `json:"latest_tag"`
}

// Check reports the latest release version and whether it is newer than
// current. It returns ("", false) when current is a dev build, on any error,
// or when current is already at or ahead of the latest release.
//
// A cache file (6h TTL) avoids repeated network calls across launches.
func Check(ctx context.Context, current string) (latest string, available bool) {
	if !isReleaseVersion(current) {
		return "", false
	}

	tag, ok := latestTag(ctx)
	if !ok {
		return "", false
	}

	if compareSemver(tag, current) > 0 {
		return normalize(tag), true
	}
	return "", false
}

// latestTag returns the latest release tag, using the cache when it is still
// fresh and otherwise fetching from GitHub (and refreshing the cache). It
// returns ok=false when no tag can be determined.
func latestTag(ctx context.Context) (tag string, ok bool) {
	if entry, err := readCache(); err == nil && time.Since(entry.CheckedAt) < cacheTTL {
		if entry.LatestTag == "" {
			return "", false
		}
		return entry.LatestTag, true
	}

	tag, ok = fetchLatestTag(ctx)
	if !ok {
		return "", false
	}
	// Best-effort cache write; ignore errors.
	_ = writeCache(cacheEntry{CheckedAt: time.Now(), LatestTag: tag})
	return tag, true
}

// fetchLatestTag performs the GitHub API request and extracts tag_name.
func fetchLatestTag(ctx context.Context) (tag string, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL(), nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", false
	}
	if payload.TagName == "" {
		return "", false
	}
	return payload.TagName, true
}

// releaseURL returns the API endpoint, allowing tests to override the target
// via the naviclaude_UPDATE_URL environment variable.
func releaseURL() string {
	if u := os.Getenv("NAVICLAUDE_UPDATE_URL"); u != "" {
		return u
	}
	return latestReleaseURL
}

// isReleaseVersion reports whether v looks like a real release version rather
// than a local/dev build.
func isReleaseVersion(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" {
		return false
	}
	_, _, _, ok := parseSemver(v)
	return ok
}

// normalize returns the tag with any leading "v" stripped for display.
func normalize(tag string) string {
	return strings.TrimPrefix(strings.TrimSpace(tag), "v")
}

// compareSemver returns >0 if a is newer than b, 0 if equal, <0 if older. A
// version that fails to parse sorts as older (returns a negative result when
// compared against a valid version), which keeps Check conservative.
func compareSemver(a, b string) int {
	amaj, amin, apat, aok := parseSemver(a)
	bmaj, bmin, bpat, bok := parseSemver(b)
	switch {
	case !aok && !bok:
		return 0
	case !aok:
		return -1
	case !bok:
		return 1
	}
	if amaj != bmaj {
		return amaj - bmaj
	}
	if amin != bmin {
		return amin - bmin
	}
	return apat - bpat
}

// parseSemver parses "v1.2.3" or "1.2.3" (a trailing pre-release/build suffix
// like "-rc1" is ignored on the patch component). It returns ok=false when the
// major/minor/patch triplet cannot be read.
func parseSemver(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return 0, 0, 0, false
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, false
	}
	// Strip any pre-release/build metadata from the patch component.
	patchStr := parts[2]
	if i := strings.IndexAny(patchStr, "-+"); i >= 0 {
		patchStr = patchStr[:i]
	}
	patch, err = strconv.Atoi(patchStr)
	if err != nil {
		return 0, 0, 0, false
	}
	return major, minor, patch, true
}

// cachePath returns the path to the update-check cache file, honoring
// NAVICLAUDE_CACHE_DIR for tests.
func cachePath() (string, error) {
	dir := os.Getenv("NAVICLAUDE_CACHE_DIR")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "naviclaude")
	}
	return filepath.Join(dir, "update-check.json"), nil
}

func readCache() (cacheEntry, error) {
	var entry cacheEntry
	path, err := cachePath()
	if err != nil {
		return entry, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return entry, err
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return entry, fmt.Errorf("parse cache: %w", err)
	}
	return entry, nil
}

func writeCache(entry cacheEntry) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
