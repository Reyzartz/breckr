// Package notifier delivers alerts over one or more user-configured channels.
//
// The shape is a transport per destination kind behind one interface, a spec per
// kind describing its config, and a dispatcher that fans an alert out to every
// channel a task selected. Adding a destination means one spec, one transport
// and one line in the registry table.
package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"breckr-server/internal/types"
)

// Message is one alert, in the shape a transport needs it.
//
// Subject exists for email, which cannot send a body alone. Chat transports
// ignore it: prefixing "[breckr] " onto a Telegram message would be noise in a
// window that already says who is talking.
type Message struct {
	Subject string
	Body    string
}

// Transport delivers a message to one destination.
//
// Send never returns an error, for the same reason the old single notifier did
// not: a delivery failure must not fail an otherwise-successful run. The caller
// needs "error" (still owed, retry) and "disabled" (nothing owed) told apart,
// which an error value cannot express.
type Transport interface {
	Send(ctx context.Context, message Message) types.NotificationOutcome
}

// newHTTPClient is what every HTTP-based transport uses, so one timeout governs
// them all and no transport can hang a run past it.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: types.NotifyTimeout}
}

// truncateTo bounds a message to what a destination accepts.
//
// Bounding by runes rather than bytes keeps the result valid UTF-8. Telegram
// counts UTF-16 code units and Discord counts characters, but every limit here
// is generous enough that runes are a safe under-approximation.
func truncateTo(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}

	suffix := []rune(types.TruncationSuffix)
	if max <= len(suffix) {
		return string(runes[:max])
	}
	return string(runes[:max-len(suffix)]) + types.TruncationSuffix
}

// fail logs the reason and returns it on the outcome, so the two can never drift
// apart -- the failure the dashboard shows is the failure the log recorded.
func fail(logger *log.Logger, format string, args ...any) types.NotificationOutcome {
	detail := fmt.Sprintf(format, args...)
	if logger != nil {
		logger.Printf("ERROR: %s", detail)
	}
	return types.NotificationOutcome{
		Delivered: false,
		Reason:    types.NotificationError,
		Detail:    detail,
	}
}

func delivered() types.NotificationOutcome {
	return types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent}
}

// postJSON is the whole delivery path for every HTTP-based transport.
//
// They differ only in endpoint, headers and payload shape, so the request
// lifecycle -- encode, bound, send, judge the status -- lives here once. `name`
// is what the failure is reported as, and it is the only thing the operator sees
// to tell one transport's error from another's.
//
// The response body is read into the failure detail because the useful reason
// lives there, not in the status text: Telegram, Slack and Discord all answer
// 400 with a body that says exactly what was wrong.
func postJSON(
	ctx context.Context,
	client *http.Client,
	logger *log.Logger,
	name string,
	method string,
	endpoint string,
	headers map[string]string,
	payload any,
) types.NotificationOutcome {
	body, err := json.Marshal(payload)
	if err != nil {
		return fail(logger, "could not encode the %s payload: %v", name, err)
	}

	ctx, cancel := context.WithTimeout(ctx, types.NotifyTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return fail(logger, "could not build the %s request: %v", name, err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := client.Do(request)
	if err != nil {
		return fail(logger,
			"failed to reach %s -- the notification will be retried on the next run: %v", name, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, types.ErrorBodyLimit))
		return fail(logger, "%s rejected the notification (%d): %s",
			name, response.StatusCode, strings.TrimSpace(string(detail)))
	}

	return delivered()
}

// timestamp is the attempt time reported to the dashboard, in the same format
// every other stored timestamp uses.
func timestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
