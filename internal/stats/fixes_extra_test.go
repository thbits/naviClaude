package stats

import "testing"

// TestFindPeakHour_DeterministicAcrossPermutations complements
// TestFindPeakHourTieBreak. It targets fix (3): findPeakHour must be fully
// deterministic on ties regardless of Go's randomized map iteration order. Because
// the input is a map, the only lever we have to perturb iteration order is to call
// the function many times on the same map and across freshly-built equivalent maps;
// the lowest tied hour must win every single time.
func TestFindPeakHour_DeterministicAcrossPermutations(t *testing.T) {
	t.Run("hour zero participates in the tie and wins as the lowest", func(t *testing.T) {
		// "00" and "13" tie; the lowest hour (0) must win, exercising the
		// count>0 guard (0 is a valid peak hour here because its count is nonzero).
		hc := map[string]int{"13": 4, "00": 4}
		for i := 0; i < 300; i++ {
			if got := findPeakHour(hc); got != 0 {
				t.Fatalf("iteration %d: findPeakHour = %d, want 0", i, got)
			}
		}
	})

	t.Run("all-zero counts yield zero, never an arbitrary hour", func(t *testing.T) {
		// No hour has a positive count, so there is no peak; the result must be the
		// neutral 0, not whichever key the map happened to visit last.
		hc := map[string]int{"7": 0, "19": 0, "23": 0}
		for i := 0; i < 300; i++ {
			if got := findPeakHour(hc); got != 0 {
				t.Fatalf("iteration %d: findPeakHour = %d, want 0", i, got)
			}
		}
	})

	t.Run("large tie set always resolves to the minimum hour", func(t *testing.T) {
		// Every hour 0..23 ties at the same count. Build the map fresh each
		// iteration so the runtime re-seeds iteration order, then assert the
		// minimum (0) wins.
		for i := 0; i < 300; i++ {
			hc := make(map[string]int, 24)
			for h := 0; h < 24; h++ {
				// Use a 2-digit string for >=10 to mirror real "HH" keys.
				key := itoa2(h)
				hc[key] = 11
			}
			if got := findPeakHour(hc); got != 0 {
				t.Fatalf("iteration %d: findPeakHour = %d, want 0", i, got)
			}
		}
	})

	t.Run("clear winner is unaffected by tie-break logic", func(t *testing.T) {
		hc := map[string]int{"3": 5, "15": 5, "21": 40}
		for i := 0; i < 200; i++ {
			if got := findPeakHour(hc); got != 21 {
				t.Fatalf("iteration %d: findPeakHour = %d, want 21", i, got)
			}
		}
	})

	t.Run("empty map yields zero", func(t *testing.T) {
		if got := findPeakHour(map[string]int{}); got != 0 {
			t.Fatalf("findPeakHour(empty) = %d, want 0", got)
		}
		if got := findPeakHour(nil); got != 0 {
			t.Fatalf("findPeakHour(nil) = %d, want 0", got)
		}
	})
}

// itoa2 renders an hour 0..23 as a zero-padded two-digit "HH" string so the parse
// loop inside findPeakHour exercises multi-digit input (e.g. "00", "07", "23").
func itoa2(h int) string {
	return string(rune('0'+h/10)) + string(rune('0'+h%10))
}
