package notifier

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"breckr-server/internal/store"
	"breckr-server/internal/types"
)

// Delivery is one channel's result within a fan-out.
type Delivery struct {
	ChannelID   string
	ChannelName string
	ChannelType types.ChannelType
	Outcome     types.NotificationOutcome
}

// Fanout is what the runner gets back: the per-channel detail to record, and one
// aggregate outcome its edge-trigger state machine can branch on unchanged.
type Fanout struct {
	Deliveries []Delivery
	Aggregate  types.NotificationOutcome
}

// Dispatcher sends an alert to every channel a task is linked to.
type Dispatcher interface {
	DispatchTask(ctx context.Context, taskID string, message Message) Fanout
	DispatchChannel(ctx context.Context, channel *store.StoredChannel, message Message) types.NotificationOutcome
}

// SQLDispatcher resolves a task's channels at send time.
//
// Deliberately a lookup rather than a field on types.ResolvedTask: the scheduler
// registry caches compiled definitions, so channel IDs baked into one would go
// stale the moment the user edited the task's channels, and the staleness would
// show up as an alert going to the wrong place.
type SQLDispatcher struct {
	channels store.ChannelStore
	logger   *log.Logger
}

func NewDispatcher(channels store.ChannelStore, logger *log.Logger) *SQLDispatcher {
	return &SQLDispatcher{channels: channels, logger: logger}
}

func (d *SQLDispatcher) DispatchTask(ctx context.Context, taskID string, message Message) Fanout {
	channels, err := d.channels.ListChannelsForTask(taskID)
	if err != nil {
		detail := fmt.Sprintf("could not load the channels for task %q: %v", taskID, err)
		d.logger.Printf("ERROR: %s", detail)
		// The alert is still owed -- a database hiccup must not be mistaken for
		// "nothing to send", which would arm the trigger and swallow it.
		return Fanout{Aggregate: types.NotificationOutcome{
			Reason: types.NotificationError,
			Detail: detail,
		}}
	}

	if len(channels) == 0 {
		d.logger.Printf("WARN: task %q has no notification channels -- the alert was logged, not sent: %s",
			taskID, message.Body)
		return Fanout{Aggregate: types.NotificationOutcome{
			Reason: types.NotificationDisabled,
			Detail: "No notification channels are attached to this task -- the alert was logged, not sent.",
		}}
	}

	deliveries := make([]Delivery, len(channels))

	var waiting sync.WaitGroup
	for i, channel := range channels {
		waiting.Add(1)
		// In parallel because a slow channel should not delay the others: the
		// sends are independent, and each is separately bounded by NotifyTimeout.
		go func(i int, channel *store.StoredChannel) {
			defer waiting.Done()
			deliveries[i] = Delivery{
				ChannelID:   channel.ID,
				ChannelName: channel.Name,
				ChannelType: channel.Type,
				Outcome:     d.DispatchChannel(ctx, channel, message),
			}
		}(i, channel)
	}
	waiting.Wait()

	return Fanout{Deliveries: deliveries, Aggregate: aggregate(deliveries)}
}

// DispatchChannel sends to one channel, building its transport on the spot.
//
// Transports are cheap and stateless, so they are built per send rather than
// cached -- which means an edited channel takes effect on the next alert with no
// invalidation to get wrong.
func (d *SQLDispatcher) DispatchChannel(
	ctx context.Context,
	channel *store.StoredChannel,
	message Message,
) types.NotificationOutcome {
	if channel.Broken {
		return fail(d.logger,
			"channel %q could not be decrypted -- re-enter its credentials in the dashboard", channel.Name)
	}

	transport, err := BuildFromConfig(channel.Type, channel.Config, d.logger)
	if err != nil {
		return fail(d.logger, "channel %q is misconfigured: %v", channel.Name, err)
	}

	return transport.Send(ctx, message)
}

// aggregate collapses the per-channel results into the single outcome the
// edge-trigger runs on.
//
// One delivery is enough to count as sent. The alternative -- retrying until
// every channel succeeds -- re-sends to the channels that already worked, and
// duplicate alerts erode trust in the alert faster than a missing one does. The
// failures are still carried in Detail and recorded per channel, so nothing is
// hidden; only the retry is given up.
func aggregate(deliveries []Delivery) types.NotificationOutcome {
	var (
		anyDelivered bool
		problems     []string
	)

	for _, delivery := range deliveries {
		if delivery.Outcome.Delivered {
			anyDelivered = true
			continue
		}
		problems = append(problems, fmt.Sprintf("%s: %s", delivery.ChannelName, delivery.Outcome.Detail))
	}

	detail := strings.Join(problems, "\n")

	if anyDelivered {
		return types.NotificationOutcome{
			Delivered: true,
			Reason:    types.NotificationSent,
			Detail:    detail,
		}
	}

	return types.NotificationOutcome{
		Reason: types.NotificationError,
		Detail: detail,
	}
}

// Attempts converts a fan-out into the rows the run history stores.
func (f Fanout) Attempts() []store.AttemptInput {
	attempts := make([]store.AttemptInput, 0, len(f.Deliveries))
	for _, delivery := range f.Deliveries {
		attempts = append(attempts, store.AttemptInput{
			ChannelID:   delivery.ChannelID,
			ChannelName: delivery.ChannelName,
			ChannelType: delivery.ChannelType,
			Status:      delivery.Outcome.Reason,
			Detail:      delivery.Outcome.Detail,
		})
	}
	return attempts
}

var _ Dispatcher = (*SQLDispatcher)(nil)
