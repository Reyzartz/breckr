package events

import (
	"reflect"
	"testing"
	"time"
)

/*
Two properties carry this package, and both are about what happens when a
dashboard is slower than the server: a burst of publishes must collapse into one
refetch, and no subscriber may ever be able to block the runner. The rest is
plumbing.
*/

// receive reads one event, failing rather than hanging if none arrives.
func receive(t *testing.T, stream <-chan Event) Event {
	t.Helper()

	select {
	case event, ok := <-stream:
		if !ok {
			t.Fatal("stream closed while waiting for an event")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

func TestSubscriberReceivesWhatWasPublished(t *testing.T) {
	bus := New()
	stream, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	bus.Publish(ResourceRuns, ResourceTasks)

	event := receive(t, stream)

	if event.Type != EventChanged {
		t.Errorf("type = %q, want %q", event.Type, EventChanged)
	}
	if want := []Resource{ResourceRuns, ResourceTasks}; !reflect.DeepEqual(event.Resources, want) {
		t.Errorf("resources = %v, want %v", event.Resources, want)
	}
}

func TestEverySubscriberSeesTheSamePublish(t *testing.T) {
	bus := New()

	first, closeFirst := bus.Subscribe()
	defer closeFirst()
	second, closeSecond := bus.Subscribe()
	defer closeSecond()

	bus.Publish(ResourceHealth)

	for name, stream := range map[string]<-chan Event{"first": first, "second": second} {
		if got := receive(t, stream).Resources; !reflect.DeepEqual(got, []Resource{ResourceHealth}) {
			t.Errorf("%s: resources = %v, want [health]", name, got)
		}
	}
}

// The reason the pending set exists: three publishes owe the client one
// refetch, not three. Asserted against the subscriber directly because through
// the bus it is a race -- the first event legitimately leaves immediately, and
// only what arrives while the sender is busy gets merged.
func TestPendingResourcesCoalesceIntoOneEvent(t *testing.T) {
	sub := newSubscriber()

	sub.publish([]Resource{ResourceRuns})
	sub.publish([]Resource{ResourceTasks})
	sub.publish([]Resource{ResourceRuns})

	want := []Resource{ResourceRuns, ResourceTasks}
	if got := sub.drain(); !reflect.DeepEqual(got, want) {
		t.Errorf("drain() = %v, want %v", got, want)
	}

	if got := sub.drain(); got != nil {
		t.Errorf("second drain() = %v, want nil -- the set should be empty", got)
	}
}

// Sorted so an event reads the same every time, in a log and in an assertion.
func TestDrainSortsResources(t *testing.T) {
	sub := newSubscriber()
	sub.publish([]Resource{ResourceTasks, ResourceChannels, ResourceRuns, ResourceHealth})

	want := []Resource{ResourceChannels, ResourceHealth, ResourceRuns, ResourceTasks}
	if got := sub.drain(); !reflect.DeepEqual(got, want) {
		t.Errorf("drain() = %v, want %v", got, want)
	}
}

// The load-bearing one: a dashboard that stopped reading must not be able to
// wedge RunTask. Nothing reads this stream at all.
func TestPublishDoesNotBlockOnAStalledSubscriber(t *testing.T) {
	bus := New()
	_, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			bus.Publish(ResourceRuns, ResourceTasks)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a subscriber that never reads")
	}
}

func TestUnsubscribeClosesTheStream(t *testing.T) {
	bus := New()
	stream, unsubscribe := bus.Subscribe()

	unsubscribe()

	select {
	case _, ok := <-stream:
		if ok {
			t.Error("received an event after unsubscribing")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream was not closed by unsubscribe")
	}
}

// Unsubscribing from an error path and again from a defer is the normal shape
// of the websocket handler, so the second call has to be a no-op.
func TestUnsubscribeIsIdempotent(t *testing.T) {
	bus := New()
	_, unsubscribe := bus.Subscribe()

	unsubscribe()
	unsubscribe()

	// A delivered publish would mean the subscriber outlived its unsubscribe.
	bus.Publish(ResourceRuns)
}

func TestPublishWithNoResourcesIsANoOp(t *testing.T) {
	bus := New()
	stream, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	bus.Publish()

	select {
	case event := <-stream:
		t.Errorf("empty publish delivered %v", event)
	case <-time.After(100 * time.Millisecond):
	}
}
