package notifier

import (
	"context"
	"log"
	"net/http"

	"breckr-server/internal/types"
)

type Telegram struct {
	spec   *TelegramSpec
	logger *log.Logger
	client *http.Client
	// baseURL is the API root. Only tests change it, to point at an httptest
	// server -- a real send must never be able to go anywhere else.
	baseURL string
}

func NewTelegram(spec *TelegramSpec, logger *log.Logger) *Telegram {
	return &Telegram{
		spec:    spec,
		logger:  logger,
		client:  newHTTPClient(),
		baseURL: types.TelegramAPIBase,
	}
}

func (t *Telegram) Send(ctx context.Context, message Message) types.NotificationOutcome {
	endpoint := t.baseURL + "/bot" + t.spec.Token + "/sendMessage"

	payload := map[string]any{
		"chat_id":                  t.spec.ChatID,
		"text":                     truncateTo(message.Body, types.TelegramMaxMessageLength),
		"disable_web_page_preview": true,
	}

	return postJSON(ctx, t.client, t.logger, "Telegram", http.MethodPost, endpoint, nil, payload)
}

var _ Transport = (*Telegram)(nil)
