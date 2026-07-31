package auth

import (
	"sync"
	"time"
)

const (
	// maxAttempts before the window closes for that caller.
	maxAttempts = 5
	// attemptWindow is how far back failures are counted.
	attemptWindow = 15 * time.Minute
	// maxKeys caps the map so a flood of spoofed sources cannot grow it without
	// bound. Evicting the oldest key when full costs a legitimate caller their
	// history at worst, which is the safe direction to be wrong in.
	maxKeys = 1024
	// FailureDelay is applied to every rejected login regardless of the window,
	// so even the attempts that stay under the limit are slow to make.
	FailureDelay = 250 * time.Millisecond
)

// Throttle is an in-memory sliding window over failed logins.
//
// In memory rather than in SQLite because it protects a single process and is
// worthless after a restart anyway: a restart clears the window, but a restart
// also means an operator is present.
type Throttle struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func NewThrottle() *Throttle {
	return &Throttle{failures: make(map[string][]time.Time)}
}

// Allow reports whether this caller may attempt a login, and if not, how long
// until their oldest counted failure ages out.
func (t *Throttle) Allow(key string) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	recent := t.prune(key, time.Now())
	if len(recent) < maxAttempts {
		return true, 0
	}

	return false, time.Until(recent[0].Add(attemptWindow))
}

// RecordFailure counts a wrong password against this caller.
func (t *Throttle) RecordFailure(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	recent := t.prune(key, now)

	if len(t.failures) >= maxKeys {
		if _, tracked := t.failures[key]; !tracked {
			t.evictOldest()
		}
	}

	t.failures[key] = append(recent, now)
}

// Reset clears the history after a successful login, so someone who mistyped
// their password four times is not one typo away from locking themselves out.
func (t *Throttle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.failures, key)
}

// prune drops failures that have aged out and returns what remains. The caller
// holds the lock.
func (t *Throttle) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-attemptWindow)

	kept := t.failures[key][:0]
	for _, at := range t.failures[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}

	if len(kept) == 0 {
		delete(t.failures, key)
		return nil
	}

	t.failures[key] = kept
	return kept
}

// evictOldest removes the entry whose most recent failure is furthest in the
// past. The caller holds the lock.
func (t *Throttle) evictOldest() {
	var oldestKey string
	var oldest time.Time

	for key, attempts := range t.failures {
		last := attempts[len(attempts)-1]
		if oldestKey == "" || last.Before(oldest) {
			oldestKey, oldest = key, last
		}
	}

	delete(t.failures, oldestKey)
}
