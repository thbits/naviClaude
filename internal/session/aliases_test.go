package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAliasStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session-names.json")

	s := NewAliasStore(path)
	if err := s.Set("sess-1", "My Session"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("sess-2", "Another"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// The file must exist after save (atomic rename completed).
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("alias file not written: %v", err)
	}

	// A fresh store reading the same path must see the persisted values.
	s2 := NewAliasStore(path)
	if err := s2.LoadErr(); err != nil {
		t.Fatalf("LoadErr after round trip: %v", err)
	}
	all := s2.All()
	if all["sess-1"] != "My Session" {
		t.Errorf("sess-1 = %q, want %q", all["sess-1"], "My Session")
	}
	if all["sess-2"] != "Another" {
		t.Errorf("sess-2 = %q, want %q", all["sess-2"], "Another")
	}
}

func TestAliasStoreDeleteOnEmptyName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "names.json")

	s := NewAliasStore(path)
	if err := s.Set("k", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("k", ""); err != nil {
		t.Fatalf("Set empty (delete): %v", err)
	}

	s2 := NewAliasStore(path)
	if _, ok := s2.All()["k"]; ok {
		t.Error("key should have been deleted by empty-name Set")
	}
}

// TestAliasStoreSaveLeavesNoTempFiles verifies the atomic-write temp file is
// renamed away, not left behind in the target directory.
func TestAliasStoreSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "names.json")

	s := NewAliasStore(path)
	if err := s.Set("a", "b"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name != "names.json" {
			t.Errorf("unexpected leftover file in dir: %q", name)
		}
	}
}

// TestAliasStoreCorruptFileRecordsError verifies a corrupt on-disk file is not
// silently treated as an empty alias set: LoadErr surfaces the unmarshal error.
func TestAliasStoreCorruptFileRecordsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "names.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	s := NewAliasStore(path)
	if s.LoadErr() == nil {
		t.Error("expected LoadErr to be non-nil for corrupt file")
	}
	// The in-memory map should still be a usable (empty) map.
	if len(s.All()) != 0 {
		t.Errorf("All() = %v, want empty for corrupt file", s.All())
	}
}

// TestAliasStoreMissingFileNoError verifies a missing file is the normal
// first-run case and not reported as corruption.
func TestAliasStoreMissingFileNoError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	s := NewAliasStore(path)
	if err := s.LoadErr(); err != nil {
		t.Errorf("LoadErr for missing file = %v, want nil", err)
	}
}
