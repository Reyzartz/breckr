package auth

import (
	"testing"
	"time"
)

func TestTheWindowClosesAfterTooManyFailures(t *testing.T) {
	throttle := NewThrottle()

	for i := range maxAttempts {
		if allowed, _ := throttle.Allow("10.0.0.1"); !allowed {
			t.Fatalf("attempt %d was refused before the limit", i+1)
		}
		throttle.RecordFailure("10.0.0.1")
	}

	allowed, retryAfter := throttle.Allow("10.0.0.1")
	if allowed {
		t.Fatalf("attempt %d was allowed despite the limit of %d", maxAttempts+1, maxAttempts)
	}
	if retryAfter <= 0 || retryAfter > attemptWindow {
		t.Fatalf("retryAfter = %v, want between 0 and %v", retryAfter, attemptWindow)
	}
}

// One caller's failures must not close the window on anyone else.
func TestTheWindowIsPerCaller(t *testing.T) {
	throttle := NewThrottle()

	for range maxAttempts {
		throttle.RecordFailure("10.0.0.1")
	}

	if allowed, _ := throttle.Allow("10.0.0.2"); !allowed {
		t.Fatal("a second caller was refused because of the first caller's failures")
	}
}

// Otherwise someone who mistyped four times is one typo away from locking
// themselves out of their own dashboard.
func TestASuccessfulLoginClearsTheHistory(t *testing.T) {
	throttle := NewThrottle()

	for range maxAttempts - 1 {
		throttle.RecordFailure("10.0.0.1")
	}
	throttle.Reset("10.0.0.1")

	for i := range maxAttempts {
		if allowed, _ := throttle.Allow("10.0.0.1"); !allowed {
			t.Fatalf("attempt %d was refused after a reset", i+1)
		}
		throttle.RecordFailure("10.0.0.1")
	}
}

func TestFailuresOlderThanTheWindowAreForgotten(t *testing.T) {
	throttle := NewThrottle()

	// Backdated rather than waited on: the window is 15 minutes.
	stale := time.Now().Add(-attemptWindow - time.Minute)
	throttle.failures["10.0.0.1"] = []time.Time{stale, stale, stale, stale, stale}

	allowed, _ := throttle.Allow("10.0.0.1")
	if !allowed {
		t.Fatal("failures older than the window still closed it")
	}
	if _, tracked := throttle.failures["10.0.0.1"]; tracked {
		t.Fatal("an entry with nothing left in the window was not dropped")
	}
}

// A flood of spoofed sources must not grow the map without bound.
func TestTheMapIsCappedAndEvictsTheOldest(t *testing.T) {
	throttle := NewThrottle()

	for i := range maxKeys {
		throttle.failures[string(rune(i))] = []time.Time{time.Now()}
	}
	// The one entry guaranteed to be evicted first.
	throttle.failures["oldest"] = []time.Time{time.Now().Add(-time.Minute)}

	throttle.RecordFailure("new-caller")

	if len(throttle.failures) > maxKeys+1 {
		t.Fatalf("the map grew to %d entries, past the cap of %d", len(throttle.failures), maxKeys)
	}
	if _, tracked := throttle.failures["oldest"]; tracked {
		t.Fatal("the least recently active entry was not the one evicted")
	}
}
