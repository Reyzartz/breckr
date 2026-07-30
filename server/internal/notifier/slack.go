package notifier

import (
	"context"
	"log"
	"net/http"

	"breckr-server/internal/types"
)

type Slack struct {
	spec   *SlackSpec
	logger *log.Logger
	client *http.Client
	// endpoint overrides the configured webhook URL. Tests only.
	endpoint string
}

func NewSlack(spec *SlackSpec, logger *log.Logger) *Slack {
	return &Slack{spec: spec, logger: logger, client: newHTTPClient()}
}

func (s *Slack) Send(ctx context.Context, message Message) types.NotificationOutcome {
	url := s.spec.WebhookURL
	if s.endpoint != "" {
		url = s.endpoint
	}

	payload := map[string]any{
		"text": truncateTo(message.Body, types.SlackMaxMessageLength),
	}

	return postJSON(ctx, s.client, s.logger, "Slack", http.MethodPost, url, nil, payload)
}

var _ Transport = (*Slack)(nil)
