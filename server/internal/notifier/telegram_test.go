package notifier

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"breckr-server/internal/types"
)

/*
Send never returns an error, so its entire contract is the outcome value. These
tests pin all three parts of it: the Delivered bool the runner branches on, the
Reason that decides whether the alert is still owed, and the Detail that is the
only thing anyone can act on once the alert has failed.

A live Telegram is never contacted -- baseURL points at an httptest server.

"disabled" is deliberately not tested here: with channels being rows, a transport
that exists is a transport that is configured, and "nothing to send to" is now the
dispatcher's judgement. See dispatcher_test.go.
*/

func newTestTelegram(t *testing.T, handler http.HandlerFunc) *Telegram {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	telegram := NewTelegram(
		&TelegramSpec{Token: "test-token", ChatID: "42"},
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

	outcome := telegram.Send(context.Background(), Message{Body: "hello"})

	if !outcome.Delivered || outcome.Reason != types.NotificationSent {
		t.Fatalf("outcome = %+v, want delivered/sent", outcome)
	}
	if outcome.Detail != "" {
		t.Fatalf("detail = %q, want empty on success", outcome.Detail)
	}
	if !strings.Contains(string(body), `"text":"hello"`) {
		t.Fatalf("body = %s, want the message", body)
	}
	if !strings.Contains(string(body), `"chat_id":"42"`) {
		t.Fatalf("body = %s, want the configured chat id", body)
	}
}

// The token belongs in the path, not the body -- a send that omits it reaches
// Telegram and is rejected as unauthorized, which reads as a bad credential
// rather than as a malformed request.
func TestTheTokenIsSentInThePath(t *testing.T) {
	var path string

	telegram := newTestTelegram(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	telegram.Send(context.Background(), Message{Body: "hello"})

	if path != "/bottest-token/sendMessage" {
		t.Fatalf("path = %q, want the token and sendMessage", path)
	}
}

func TestARejectionCarriesTelegramsOwnReason(t *testing.T) {
	telegram := newTestTelegram(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	})

	outcome := telegram.Send(context.Background(), Message{Body: "hello"})

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

func TestAnUnreachableTransportIsAnError(t *testing.T) {
	telegram := NewTelegram(
		&TelegramSpec{Token: "test-token", ChatID: "42"},
		log.New(io.Discard, "", 0),
	)
	// A port nothing listens on: the send fails before it gets an answer.
	telegram.baseURL = "http://127.0.0.1:1"

	outcome := telegram.Send(context.Background(), Message{Body: "hello"})

	// Must be "error", not "disabled" -- the alert is still owed, and the runner
	// leaves the task disarmed so the next run retries it.
	if outcome.Reason != types.NotificationError {
		t.Fatalf("reason = %q, want error", outcome.Reason)
	}
	if outcome.Detail == "" {
		t.Fatal("an unreachable transport must still say why")
	}
}

func TestAnOverlongMessageIsTruncatedNotRejected(t *testing.T) {
	var body []byte

	telegram := newTestTelegram(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	outcome := telegram.Send(context.Background(),
		Message{Body: strings.Repeat("a", types.TelegramMaxMessageLength+500)})

	if !outcome.Delivered {
		t.Fatalf("outcome = %+v, want delivered", outcome)
	}
	if !strings.Contains(string(body), "truncated") {
		t.Fatal("an overlong body is truncated rather than rejected by Telegram")
	}
}
