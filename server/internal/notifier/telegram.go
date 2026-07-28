// Package notifier delivers alerts. Telegram is the only transport.
package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"breckr-server/internal/config"
	"breckr-server/internal/types"
)

// Notifier is the seam the runner is written against, so its edge-trigger state
// machine can be tested against every delivery outcome without a network.
type Notifier interface {
	Send(ctx context.Context, message string) types.NotificationOutcome
}

type Telegram struct {
	cfg    config.TelegramConfig
	logger *log.Logger
	client *http.Client
}

func NewTelegram(cfg config.TelegramConfig, logger *log.Logger) *Telegram {
	return &Telegram{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: types.TelegramTimeout},
	}
}

func truncate(text string) string {
	// Telegram counts UTF-16 code units, but the limit is generous enough that
	// bounding by runes is both safe and keeps the message valid UTF-8.
	runes := []rune(text)
	if len(runes) <= types.TelegramMaxMessageLength {
		return text
	}
	keep := types.TelegramMaxMessageLength - len([]rune(types.TelegramTruncationSuffix))
	return string(runes[:keep]) + types.TelegramTruncationSuffix
}

// Send delivers a message, and never returns an error.
//
// A notification failure must not fail an otherwise-successful run, and must not
// prevent the run being recorded. The caller instead needs to tell two
// non-delivery cases apart, because they demand opposite handling of the
// edge-trigger state:
//
//	"error"    -- Telegram was configured but the send broke. The alert is still
//	              owed, so the caller must NOT advance the armed state; the next
//	              run retries.
//	"disabled" -- no token configured, so there is nothing to retry and nothing
//	              owed. The caller advances state as if sent, which keeps dedup
//	              behaving identically with and without Telegram set up.
func (t *Telegram) Send(ctx context.Context, message string) types.NotificationOutcome {
	text := truncate(message)

	if !t.cfg.Enabled {
		t.logger.Printf("WARN: Telegram not configured -- notification logged instead of sent: %s", text)
		return types.NotificationOutcome{Delivered: false, Reason: types.NotificationDisabled}
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", types.TelegramAPIBase, t.cfg.Token)

	body, err := json.Marshal(map[string]any{
		"chat_id":                  t.cfg.ChatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	if err != nil {
		t.logger.Printf("ERROR: could not encode the Telegram payload: %v", err)
		return types.NotificationOutcome{Delivered: false, Reason: types.NotificationError}
	}

	ctx, cancel := context.WithTimeout(ctx, types.TelegramTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.logger.Printf("ERROR: could not build the Telegram request: %v", err)
		return types.NotificationOutcome{Delivered: false, Reason: types.NotificationError}
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := t.client.Do(request)
	if err != nil {
		t.logger.Printf("ERROR: failed to reach Telegram -- notification will be retried on the next run: %v", err)
		return types.NotificationOutcome{Delivered: false, Reason: types.NotificationError}
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Telegram puts the useful reason in the body, not the status text.
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 500))
		t.logger.Printf("ERROR: Telegram rejected the notification (%d): %s", response.StatusCode, detail)
		return types.NotificationOutcome{Delivered: false, Reason: types.NotificationError}
	}

	return types.NotificationOutcome{Delivered: true, Reason: types.NotificationSent}
}

// Ensure the concrete type keeps satisfying the interface the runner holds.
var _ Notifier = (*Telegram)(nil)
