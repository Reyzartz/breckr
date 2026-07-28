package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"breckr-server/internal/config"
	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

// Notifier is the slice of the notifier this route needs.
type Notifier interface {
	Send(ctx context.Context, message string) types.NotificationOutcome
}

type NotificationHandler struct {
	logger   *log.Logger
	notifier Notifier
	cfg      *config.Config
}

func NewNotificationHandler(
	logger *log.Logger,
	notifier Notifier,
	cfg *config.Config,
) *NotificationHandler {
	return &NotificationHandler{logger: logger, notifier: notifier, cfg: cfg}
}

// HandleTestNotification sends one real notification, on demand.
//
// It exists because the alternative way to find out whether alerts work is to
// author a task, wait for its condition to actually fire, and see whether
// anything arrives -- by which point a misconfiguration has already cost the
// alert it was meant to deliver.
//
// This deliberately goes through the same notifier the runner uses rather than
// re-checking the config: a token that parses but the API rejects is exactly
// the failure a config check would miss.
func (nh *NotificationHandler) HandleTestNotification(w http.ResponseWriter, r *http.Request) {
	// Not r.Context(): a client that navigates away mid-request would otherwise
	// cancel the send, and report a delivery failure that never happened.
	ctx, cancel := context.WithTimeout(context.Background(), types.TelegramTimeout)
	defer cancel()

	outcome := nh.notifier.Send(ctx, types.TestNotificationMessage)

	nh.logger.Printf("INFO: test notification attempted (status=%s delivered=%t)",
		outcome.Reason, outcome.Delivered)

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{
		"data": types.TestNotificationResponse{
			OK:          outcome.Delivered,
			Status:      outcome.Reason,
			Detail:      outcome.Detail,
			Message:     types.TestNotificationMessage,
			AttemptedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
}
