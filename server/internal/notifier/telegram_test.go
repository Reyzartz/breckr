package notifier

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"breckr-server/internal/config"
	"breckr-server/internal/types"
)

/*
Send never returns an error, so its entire contract is the outcome value. These
tests pin all three parts of it: the Delivered bool the runner branches on, the
Reason that decides whether the alert is still owed, and the Detail that is the
only thing anyone can act on once the alert has failed.

A live Telegram is never contacted -- baseURL points at an httptest server.
*/

func newTestTelegram(t *testing.T, handler http.HandlerFunc) *Telegram {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	telegram := NewTelegram(
		config.TelegramConfig{Token: "test-token", ChatID: "42", Enabled: true},
		log.New(io.Discard, "", 0),
	)
	telegram.baseURL = server.URL

	return telegram
}

func TestASuccessfulSendReportsDelivered(t *testing.T) {
	var body []byte

	telegram := newTestTelegram(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	outcome := telegram.Send(context.Background(), "hello")

	if !outcome.Delivered || outcome.Reason != types.NotificationSent {
		t.Fatalf("outcome = %+v, want delivered/sent", outcome)
	}
	if outcome.Detail != "" {
		t.Fatalf("detail = %q, want empty on success", outcome.Detail)
	}
	if !strings.Contains(string(body), `"text":"hello"`) {
		t.Fatalf("body = %s, want the message", body)
	}
}

func TestARejectionCarriesTelegramsOwnReason(t *testing.T) {
	telegram := newTestTelegram(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	})

	outcome := telegram.Send(context.Background(), "hello")

	if outcome.Delivered || outcome.Reason != types.NotificationError {
		t.Fatalf("outcome = %+v, want undelivered/error", outcome)
	}
	// The status code alone says nothing actionable; Telegram puts the useful
	// part in the body, so that is what has to reach the run row.
	if !strings.Contains(outcome.Detail, "chat not found") {
		t.Fatalf("detail = %q, want the response body", outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "400") {
		t.Fatalf("detail = %q, want the status code", outcome.Detail)
	}
}

func TestAnUnreachableTransportIsAnErrorNotADisabledNotifier(t *testing.T) {
	telegram := NewTelegram(
		config.TelegramConfig{Token: "test-token", ChatID: "42", Enabled: true},
		log.New(io.Discard, "", 0),
	)
	// A port nothing listens on: the send fails before it gets an answer.
	telegram.baseURL = "http://127.0.0.1:1"

	outcome := telegram.Send(context.Background(), "hello")

	// Must be "error", not "disabled" -- the alert is still owed, and the
	// runner leaves the task disarmed so the next run retries it.
	if outcome.Reason != types.NotificationError {
		t.Fatalf("reason = %q, want error", outcome.Reason)
	}
	if outcome.Detail == "" {
		t.Fatal("an unreachable transport must still say why")
	}
}

func TestAnUnconfiguredNotifierReportsDisabled(t *testing.T) {
	telegram := NewTelegram(
		config.TelegramConfig{Enabled: false},
		log.New(io.Discard, "", 0),
	)

	outcome := telegram.Send(context.Background(), "hello")

	if outcome.Delivered || outcome.Reason != types.NotificationDisabled {
		t.Fatalf("outcome = %+v, want undelivered/disabled", outcome)
	}
	// Nothing is owed here, so the detail is the fix rather than a fault.
	if !strings.Contains(outcome.Detail, "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("detail = %q, want it to name what is missing", outcome.Detail)
	}
}

func TestAnOverlongMessageIsTruncatedNotRejected(t *testing.T) {
	var body []byte

	telegram := newTestTelegram(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	outcome := telegram.Send(context.Background(), strings.Repeat("a", types.TelegramMaxMessageLength+500))

	if !outcome.Delivered {
		t.Fatalf("outcome = %+v, want delivered", outcome)
	}
	if !strings.Contains(string(body), "truncated") {
		t.Fatal("an overlong body is truncated rather than rejected by Telegram")
	}
}
