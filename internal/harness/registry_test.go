package harness

import (
	"testing"

	"github.com/thbits/naviClaude/internal/session"
)

func TestRegistryLookup(t *testing.T) {
	a := &coreHarness{name: "a"}
	b := &coreHarness{name: "b"}
	r := NewRegistry(a, b)

	if got := r.All(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("All() order wrong: %v", got)
	}
	if r.Default() != a {
		t.Errorf("Default() should be first registered")
	}
	if h, ok := r.Get("b"); !ok || h != b {
		t.Errorf("Get(b) = %v, %v", h, ok)
	}
	if _, ok := r.Get("zzz"); ok {
		t.Errorf("Get(unknown) should be false")
	}
	if h, ok := r.For(&session.Session{}); !ok || h != a {
		t.Errorf("For(unstamped) should resolve to default")
	}
	if h, ok := r.For(&session.Session{Harness: "b"}); !ok || h != b {
		t.Errorf("For(b) = %v, %v", h, ok)
	}
	if _, ok := r.For(&session.Session{Harness: "zzz"}); ok {
		t.Errorf("For(unknown harness) should be false")
	}
	if _, ok := r.For(nil); ok {
		t.Errorf("For(nil) should be false")
	}
}

func TestRegistryRejectsDuplicates(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate harness name")
		}
	}()
	NewRegistry(&coreHarness{name: "a"}, &coreHarness{name: "a"})
}

func TestCapabilityHelpers(t *testing.T) {
	core := &coreHarness{name: "core"}
	full := &fullHarness{coreHarness{name: "full"}}

	checks := []struct {
		name string
		fn   func(Harness) bool
	}{
		{"Resumer", func(h Harness) bool { _, ok := AsResumer(h); return ok }},
		{"Forker", func(h Harness) bool { _, ok := AsForker(h); return ok }},
		{"History", func(h Harness) bool { _, ok := AsHistory(h); return ok }},
		{"Conversation", func(h Harness) bool { _, ok := AsConversation(h); return ok }},
		{"Metrics", func(h Harness) bool { _, ok := AsMetrics(h); return ok }},
		{"ChangedFiles", func(h Harness) bool { _, ok := AsChangedFiles(h); return ok }},
		{"Stats", func(h Harness) bool { _, ok := AsStats(h); return ok }},
	}
	for _, c := range checks {
		if c.fn(core) {
			t.Errorf("core harness should not be a %s", c.name)
		}
		if !c.fn(full) {
			t.Errorf("full harness should be a %s", c.name)
		}
	}
}
