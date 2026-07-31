// Package events carries a "something changed" signal from the write paths to
// every connected dashboard.
//
// Deliberately a signal and not a payload: subscribers refetch through the same
// HTTP routes they already use, which keeps run filtering, pagination and
// totals server-side rather than mirrored into a second copy that can drift.
package events

import (
	"sort"
	"sync"
)

// Resource names one slice of dashboard state. A subscriber refetches only the
// resources an event names, so a finished run does not re-probe the browser.
type Resource string

const (
	ResourceTasks    Resource = "tasks"
	ResourceRuns     Resource = "runs"
	ResourceChannels Resource = "channels"
	ResourceHealth   Resource = "health"
)

// EventChanged is the only type today. Named rather than inlined so the client
// has something to compare against and can ignore a type it does not know.
const EventChanged = "changed"

// Event is the whole wire contract.
type Event struct {
	Type      string     `json:"type"`
	Resources []Resource `json:"resources"`
}

// Bus fans a publish out to every live subscriber.
//
// Publish is non-blocking and never fails, because its callers are the runner
// and the request handlers: a dashboard that has stopped reading must not be
// able to stall a task run.
type Bus struct {
	mu     sync.Mutex
	subs   map[int64]*subscriber
	nextID int64
}

func New() *Bus {
	return &Bus{subs: make(map[int64]*subscriber)}
}

// Subscribe returns a stream of events and the function that ends it. The
// stream is closed once unsubscribe runs, so a ranging consumer terminates.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	sub := newSubscriber()

	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.subs[id] = sub
	b.mu.Unlock()

	go sub.run()

	return sub.out, func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()

		sub.stop()
	}
}

// Publish records that the named resources changed.
//
// Safe to call from anywhere, including while holding no locks of interest:
// it copies the subscriber list, merges into each one's pending set, and
// returns. Nothing here waits on a socket.
func (b *Bus) Publish(resources ...Resource) {
	if len(resources) == 0 {
		return
	}

	b.mu.Lock()
	targets := make([]*subscriber, 0, len(b.subs))
	for _, sub := range b.subs {
		targets = append(targets, sub)
	}
	b.mu.Unlock()

	for _, sub := range targets {
		sub.publish(resources)
	}
}

// subscriber coalesces rather than buffers.
//
// A burst of run completions must not become a burst of refetches, so pending
// resources are merged into a set and the wake-up is a single capacity-1
// channel. Publishing is then O(1) and allocation-free in the steady state, and
// it can neither block nor drop: an already-queued wake-up stands for whatever
// the set holds when the sender gets around to reading it.
type subscriber struct {
	mu      sync.Mutex
	pending map[Resource]struct{}

	notify chan struct{}
	out    chan Event
	done   chan struct{}
	once   sync.Once
}

func newSubscriber() *subscriber {
	return &subscriber{
		pending: make(map[Resource]struct{}),
		notify:  make(chan struct{}, 1),
		out:     make(chan Event),
		done:    make(chan struct{}),
	}
}

func (s *subscriber) publish(resources []Resource) {
	s.mu.Lock()
	for _, resource := range resources {
		s.pending[resource] = struct{}{}
	}
	s.mu.Unlock()

	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// drain empties the pending set into one sorted slice.
//
// Sorted because map iteration is random, and an event whose field order
// wobbles is needlessly awkward to assert on and to read in a log.
func (s *subscriber) drain() []Resource {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pending) == 0 {
		return nil
	}

	resources := make([]Resource, 0, len(s.pending))
	for resource := range s.pending {
		resources = append(resources, resource)
		delete(s.pending, resource)
	}

	sort.Slice(resources, func(i, j int) bool { return resources[i] < resources[j] })
	return resources
}

func (s *subscriber) run() {
	defer close(s.out)

	for {
		select {
		case <-s.done:
			return
		case <-s.notify:
			resources := s.drain()
			if len(resources) == 0 {
				continue
			}

			// Blocking is fine and is the point: while this send waits on a
			// slow reader, further publishes pile into the pending set and
			// arrive as one event rather than a queue of stale ones.
			select {
			case s.out <- Event{Type: EventChanged, Resources: resources}:
			case <-s.done:
				return
			}
		}
	}
}

// stop is idempotent -- a handler that unsubscribes on an error path and again
// from a defer is the normal shape, not a bug.
func (s *subscriber) stop() {
	s.once.Do(func() { close(s.done) })
}
