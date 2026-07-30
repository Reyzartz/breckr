package notifier

import (
	"context"
	"log"
	"net/http"

	"breckr-server/internal/types"
)

// Webhook posts the alert to an arbitrary endpoint, for anything the built-in
// transports do not cover.
//
// The payload is a stable, documented shape rather than a mirror of one chat
// service's format -- whatever is on the other end is writing a parser against
// it.
type Webhook struct {
	spec   *WebhookSpec
	logger *log.Logger
	client *http.Client
	// endpoint overrides the configured URL. Tests only.
	endpoint string
}

func NewWebhook(spec *WebhookSpec, logger *log.Logger) *Webhook {
	return &Webhook{spec: spec, logger: logger, client: newHTTPClient()}
}

func (w *Webhook) Send(ctx context.Context, message Message) types.NotificationOutcome {
	url := w.spec.URL
	if w.endpoint != "" {
		url = w.endpoint
	}

	payload := map[string]any{
		"subject": message.Subject,
		"message": message.Body,
		"sent_at": timestamp(),
		"source":  types.WebhookSource,
	}

	// Lowercase because the name is interpolated mid-sentence ("failed to reach
	// the webhook"), matching how every other detail reads.
	return postJSON(ctx, w.client, w.logger, "the webhook",
		w.spec.method(), url, w.spec.Headers, payload)
}

var _ Transport = (*Webhook)(nil)
