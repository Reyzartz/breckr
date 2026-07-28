package browser

import (
	"errors"
	"sync"
	"testing"
	"time"

	"breckr-server/internal/config"
	"breckr-server/internal/types"
)

/*
The mutex and the timeout, exercised through WithoutPage so no CDP server is
needed.

Lightpanda's CDP server accepts one connection, one context and one page per
process, so two tasks whose schedules collide would fight over it. The
scheduler's SkipIfStillRunning only stops a task overlapping itself; this is what
covers the cross-task case.
*/

func newPool() *Pool {
	return NewPool(&config.Config{})
}

func TestWithoutPageSerializesWork(t *testing.T) {
	pool := newPool()

	var (
		mu      sync.Mutex
		active  int
		maxSeen int
		wg      sync.WaitGroup
	)

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pool.WithoutPage(5*time.Second, func() error {
				mu.Lock()
				active++
				if active > maxSeen {
					maxSeen = active
				}
				mu.Unlock()

				time.Sleep(5 * time.Millisecond)

				mu.Lock()
				active--
				mu.Unlock()
				return nil
			})
		}()
	}

	wg.Wait()

	if maxSeen != 1 {
		t.Fatalf("saw %d concurrent runs, want 1", maxSeen)
	}
}

// One failure must not poison the queue for every later caller.
func TestAFailedRunDoesNotPoisonTheQueue(t *testing.T) {
	pool := newPool()

	boom := errors.New("boom")
	if err := pool.WithoutPage(time.Second, func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("the caller should see its own error, got %v", err)
	}

	ran := false
	if err := pool.WithoutPage(time.Second, func() error { ran = true; return nil }); err != nil {
		t.Fatalf("the next run should still work, got %v", err)
	}
	if !ran {
		t.Fatal("the next run never executed")
	}
}

func TestWithoutPageTimesOut(t *testing.T) {
	pool := newPool()

	startedAt := time.Now()
	err := pool.WithoutPage(100*time.Millisecond, func() error {
		time.Sleep(3 * time.Second)
		return nil
	})
	elapsed := time.Since(startedAt)

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if elapsed > time.Second {
		t.Fatalf("should have given up promptly, took %s", elapsed)
	}
}

func TestAFastRunDoesNotWaitForItsTimeout(t *testing.T) {
	pool := newPool()

	startedAt := time.Now()
	if err := pool.WithoutPage(5*time.Second, func() error { return nil }); err != nil {
		t.Fatalf("WithoutPage: %v", err)
	}

	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("a fast run should return immediately, took %s", elapsed)
	}
}

// A run that is still going when its timeout fires must not hold the mutex --
// otherwise one hung task would stall every later one until it finished.
func TestATimedOutRunReleasesTheMutex(t *testing.T) {
	pool := newPool()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	_ = pool.WithoutPage(50*time.Millisecond, func() error {
		<-release
		return nil
	})

	done := make(chan struct{})
	go func() {
		_ = pool.WithoutPage(time.Second, func() error { return nil })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a timed-out run is still holding the mutex")
	}
}

// A browser that is down makes connect hang, which is exactly the case a run
// timeout must catch -- so the timeout has to cover connection, not just
// execution.
func TestWithPageTimesOutOnAnUnreachableBrowser(t *testing.T) {
	pool := NewPool(&config.Config{
		Browser: config.BrowserConfig{
			Endpoint:   "ws://127.0.0.1:1",
			ControlURL: "ws://127.0.0.1:1",
		},
	})

	called := false
	err := pool.WithPage(500*time.Millisecond, func(types.Page) error {
		called = true
		return nil
	})

	if err == nil {
		t.Fatal("connecting to a dead endpoint must fail")
	}
	if called {
		t.Fatal("the task body must not run without a page")
	}
}

func TestCheckReachableReportsAnUnreachableBrowser(t *testing.T) {
	pool := NewPool(&config.Config{
		Browser: config.BrowserConfig{
			Endpoint:   "ws://127.0.0.1:1",
			ControlURL: "ws://127.0.0.1:1",
		},
	})

	health := pool.CheckReachable(500 * time.Millisecond)

	if health.Reachable {
		t.Fatal("a dead endpoint is not reachable")
	}
	if health.Error == "" {
		t.Fatal("the reason should be reported, not swallowed")
	}
	if health.Endpoint != "ws://127.0.0.1:1" {
		t.Fatalf("endpoint = %q", health.Endpoint)
	}
}
