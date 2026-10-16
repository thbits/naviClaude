package session

import (
	"sync"
	"testing"
	"time"
)

// TestStatusTrackerResolveConcurrent hammers StatusTracker.Resolve and Prune
// from many goroutines across overlapping targets. Resolve writes lastWorking
// under st.mu and Prune deletes from it under the same lock; this verifies the
// guarded map access is race-free under concurrent reads, writes, and deletes.
func TestStatusTrackerResolveConcurrent(t *testing.T) {
	st := NewStatusTracker(10 * time.Second)

	targets := []string{"a:0.0", "b:0.0", "c:0.0", "d:0.0", "e:0.0"}
	base := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	// Resolvers: each goroutine cycles through every target with a mix of
	// signals, writing into and reading from lastWorking concurrently.
	for g := 0; g < 24; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			signals := []PaneSignal{SignalWorking, SignalNone, SignalWaiting}
			for r := 0; r < 200; r++ {
				for i, tgt := range targets {
					sig := signals[(g+r+i)%len(signals)]
					now := base.Add(time.Duration(r) * time.Second)
					_ = st.Resolve(tgt, sig, (g+i)%2 == 0, (r+i)%2 == 0, now)
				}
			}
		}(g)
	}
	// Pruners: concurrently drop a rotating subset of targets so deletes race
	// with the resolvers' inserts/reads on the same map.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for r := 0; r < 200; r++ {
				active := map[string]bool{targets[(g+r)%len(targets)]: true}
				st.Prune(active)
			}
		}(g)
	}
	wg.Wait()

	// Sanity: a deterministic call still returns the expected status after the
	// concurrent storm (state machine remains intact).
	if got := st.Resolve("a:0.0", SignalWaiting, false, false, base); got != StatusWaiting {
		t.Errorf("post-storm Resolve waiting = %v, want StatusWaiting", got)
	}
}
