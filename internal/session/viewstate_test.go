package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestViewStateSaveAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "view-state.json")
	s := NewViewStateStore(path)

	want := ViewState{
		LastSessionID:   "uuid-123",
		CollapsedGroups: map[string]bool{"hermes": true, "default": false},
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// A fresh store reading the same file must see the persisted values.
	got := NewViewStateStore(path).Get()
	if got.LastSessionID != "uuid-123" {
		t.Errorf("LastSessionID = %q, want %q", got.LastSessionID, "uuid-123")
	}
	if got.CollapsedGroups["hermes"] != true || got.CollapsedGroups["default"] != false {
		t.Errorf("CollapsedGroups = %v, want hermes:true default:false", got.CollapsedGroups)
	}
}

func TestViewStateMissingFile(t *testing.T) {
	s := NewViewStateStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	got := s.Get()
	if got.LastSessionID != "" || len(got.CollapsedGroups) != 0 {
		t.Errorf("missing file should yield zero ViewState, got %+v", got)
	}
	if s.LoadErr() != nil {
		t.Errorf("missing file should not be an error, got %v", s.LoadErr())
	}
}

func TestViewStateCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "view-state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewViewStateStore(path)
	if s.LoadErr() == nil {
		t.Error("corrupt file should surface via LoadErr")
	}
}

func TestViewStateGetReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	s := NewViewStateStore(filepath.Join(dir, "view-state.json"))
	if err := s.Save(ViewState{CollapsedGroups: map[string]bool{"a": true}}); err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	got.CollapsedGroups["a"] = false // mutate the copy
	if s.Get().CollapsedGroups["a"] != true {
		t.Error("Get() must return a copy; internal state was mutated")
	}
}
