package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"breckr-server/internal/store"
	"breckr-server/internal/types"
)

/*
The aggregation rule is the load-bearing decision in this package: it is what the
runner's edge-trigger branches on, so getting it wrong means either a swallowed
alert or a duplicated one. These tests pin all four cases.
*/

// fakeChannels serves a fixed channel list, so the aggregation can be tested
// without a database or a network.
type fakeChannels struct {
	store.ChannelStore
	channels []*store.StoredChannel
	err      error
}

func (f *fakeChannels) ListChannelsForTask(string) ([]*store.StoredChannel, error) {
	return f.channels, f.err
}

// channel builds a stored channel pointed at a URL, which is what decides
// whether its send succeeds.
func channel(name, url string) *store.StoredChannel {
	config, _ := json.Marshal(DiscordSpec{WebhookURL: url})
	return &store.StoredChannel{
		Channel: types.Channel{
			ID:      name,
			Name:    name,
			Type:    types.ChannelDiscord,
			Enabled: true,
		},
		Config: config,
	}
}

func dispatch(t *testing.T, channels ...*store.StoredChannel) Fanout {
	t.Helper()

	dispatcher := NewDispatcher(&fakeChannels{channels: channels}, discard())
	return dispatcher.DispatchTask(context.Background(), "task", Message{Body: "hello"})
}

// The whole point of fanning out: every channel gets the alert.
func TestEverySelectedChannelReceivesTheAlert(t *testing.T) {
	first, firstSeen := capture(t)
	second, secondSeen := capture(t)

	fanout := dispatch(t, channel("first", first.URL), channel("second", second.URL))

	if !fanout.Aggregate.Delivered || fanout.Aggregate.Reason != types.NotificationSent {
		t.Fatalf("aggregate = %+v, want delivered/sent", fanout.Aggregate)
	}
	if len(firstSeen.body) == 0 || len(secondSeen.body) == 0 {
		t.Fatal("both channels must receive the alert, not just the first")
	}
	if len(fanout.Deliveries) != 2 {
		t.Fatalf("got %d deliveries, want one per channel", len(fanout.Deliveries))
	}
}

// One success is enough. Retrying for the sake of the failed channel would
// re-alert the one that already worked, and duplicate alerts erode trust in the
// alert faster than a missing one does.
func TestOneDeliveryIsEnoughToCountAsSent(t *testing.T) {
	working, _ := capture(t)

	fanout := dispatch(t,
		channel("working", working.URL),
		channel("broken", "http://127.0.0.1:1"),
	)

	if !fanout.Aggregate.Delivered || fanout.Aggregate.Reason != types.NotificationSent {
		t.Fatalf("aggregate = %+v, want delivered/sent despite the failure", fanout.Aggregate)
	}
	// Delivered, but the failure is not swept under the rug -- it still has to be
	// visible, or a permanently broken channel goes unnoticed forever.
	if !strings.Contains(fanout.Aggregate.Detail, "broken") {
		t.Fatalf("detail = %q, want it to name the channel that failed", fanout.Aggregate.Detail)
	}
}

// Nothing got through, so the alert is still owed: the runner must leave the task
// disarmed and retry on the next run.
func TestEveryChannelFailingIsAnError(t *testing.T) {
	fanout := dispatch(t,
		channel("one", "http://127.0.0.1:1"),
		channel("two", "http://127.0.0.1:1"),
	)

	if fanout.Aggregate.Delivered || fanout.Aggregate.Reason != types.NotificationError {
		t.Fatalf("aggregate = %+v, want undelivered/error", fanout.Aggregate)
	}
	for _, name := range []string{"one", "two"} {
		if !strings.Contains(fanout.Aggregate.Detail, name) {
			t.Fatalf("detail = %q, want every failure named", fanout.Aggregate.Detail)
		}
	}
}

// "disabled" rather than "error": nothing is owed, so the runner arms the trigger
// as if sent. Treating it as an error would retry forever on a task the user
// never attached a channel to.
func TestATaskWithNoChannelsIsDisabledNotAnError(t *testing.T) {
	fanout := dispatch(t)

	if fanout.Aggregate.Delivered || fanout.Aggregate.Reason != types.NotificationDisabled {
		t.Fatalf("aggregate = %+v, want undelivered/disabled", fanout.Aggregate)
	}
	if len(fanout.Deliveries) != 0 {
		t.Fatalf("got %d deliveries, want none", len(fanout.Deliveries))
	}
	if !strings.Contains(fanout.Aggregate.Detail, "No notification channels") {
		t.Fatalf("detail = %q, want it to explain that nothing is attached", fanout.Aggregate.Detail)
	}
}

// A database hiccup must not be mistaken for "nothing to send": that would arm
// the trigger and swallow the alert permanently.
func TestAFailedChannelLookupIsAnError(t *testing.T) {
	dispatcher := NewDispatcher(&fakeChannels{err: errors.New("database is locked")}, discard())

	fanout := dispatcher.DispatchTask(context.Background(), "task", Message{Body: "hello"})

	if fanout.Aggregate.Reason != types.NotificationError {
		t.Fatalf("reason = %q, want error so the alert is retried", fanout.Aggregate.Reason)
	}
}

// A replaced key file leaves rows that cannot be decrypted. The channel must
// report a real failure rather than being skipped, or the alert silently goes
// nowhere.
func TestABrokenChannelFailsLoudly(t *testing.T) {
	broken := &store.StoredChannel{
		Channel: types.Channel{
			ID:      "broken",
			Name:    "stale-key",
			Type:    types.ChannelDiscord,
			Enabled: true,
			Broken:  true,
		},
	}

	fanout := dispatch(t, broken)

	if fanout.Aggregate.Reason != types.NotificationError {
		t.Fatalf("reason = %q, want error", fanout.Aggregate.Reason)
	}
	if !strings.Contains(fanout.Aggregate.Detail, "stale-key") {
		t.Fatalf("detail = %q, want it to name the channel to fix", fanout.Aggregate.Detail)
	}
}

// The per-channel rows are what answer "which one failed" after the fact, so the
// fan-out has to convert cleanly into them.
func TestAttemptsCarryEveryChannelsOutcome(t *testing.T) {
	working, _ := capture(t)

	fanout := dispatch(t,
		channel("working", working.URL),
		channel("broken", "http://127.0.0.1:1"),
	)

	attempts := fanout.Attempts()
	if len(attempts) != 2 {
		t.Fatalf("got %d attempts, want one per channel", len(attempts))
	}

	byName := map[string]store.AttemptInput{}
	for _, attempt := range attempts {
		byName[attempt.ChannelName] = attempt
	}

	if byName["working"].Status != types.NotificationSent {
		t.Fatalf("working status = %q, want sent", byName["working"].Status)
	}
	if byName["broken"].Status != types.NotificationError {
		t.Fatalf("broken status = %q, want error", byName["broken"].Status)
	}
	if byName["broken"].Detail == "" {
		t.Fatal("a failed attempt must record why")
	}
}
