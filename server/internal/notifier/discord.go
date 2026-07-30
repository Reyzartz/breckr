package notifier

import (
	"context"
	"log"
	"net/http"

	"breckr-server/internal/types"
)

type Discord struct {
	spec   *DiscordSpec
	logger *log.Logger
	client *http.Client
	// endpoint overrides the configured webhook URL. Tests only.
	endpoint string
}

func NewDiscord(spec *DiscordSpec, logger *log.Logger) *Discord {
	return &Discord{spec: spec, logger: logger, client: newHTTPClient()}
}

func (d *Discord) Send(ctx context.Context, message Message) types.NotificationOutcome {
	url := d.spec.WebhookURL
	if d.endpoint != "" {
		url = d.endpoint
	}

	payload := map[string]any{
		"content": truncateTo(message.Body, types.DiscordMaxMessageLength),
	}

	return postJSON(ctx, d.client, d.logger, "Discord", http.MethodPost, url, nil, payload)
}

var _ Transport = (*Discord)(nil)
