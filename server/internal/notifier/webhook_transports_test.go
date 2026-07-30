package notifier

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"breckr-server/internal/types"
)

/*
The three webhook-shaped transports differ only in the JSON they post, and that
payload shape is the contract the receiving service parses. These tests pin the
field names, since a renamed key fails silently -- Slack answers 200 to a body it
does not understand.
*/

// captured is what a transport actually put on the wire.
type captured struct {
	method  string
	headers http.Header
	body    []byte
}

// capture stands up a server that records the request, so each test asserts on
// what was sent rather than on the transport's own report of it.
func capture(t *testing.T) (*httptest.Server, *captured) {
	t.Helper()

	seen := &captured{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method = r.Method
		seen.headers = r.Header.Clone()
		seen.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	return server, seen
}

// decode reads the captured body as JSON, failing the test if it is not.
func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()

	var payload T
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return payload
}

func discard() *log.Logger { return log.New(io.Discard, "", 0) }

func TestDiscordPostsItsContentField(t *testing.T) {
	server, seen := capture(t)

	discord := NewDiscord(&DiscordSpec{WebhookURL: server.URL}, discard())
	outcome := discord.Send(context.Background(), Message{Body: "the sky is falling"})

	if !outcome.Delivered {
		t.Fatalf("outcome = %+v, want delivered", outcome)
	}

	payload := decode[map[string]any](t, seen.body)
	if payload["content"] != "the sky is falling" {
		t.Fatalf("payload = %v, want the message under \"content\"", payload)
	}
}

func TestSlackPostsItsTextField(t *testing.T) {
	server, seen := capture(t)

	slack := NewSlack(&SlackSpec{WebhookURL: server.URL}, discard())
	outcome := slack.Send(context.Background(), Message{Body: "the sky is falling"})

	if !outcome.Delivered {
		t.Fatalf("outcome = %+v, want delivered", outcome)
	}

	payload := decode[map[string]any](t, seen.body)
	if payload["text"] != "the sky is falling" {
		t.Fatalf("payload = %v, want the message under \"text\"", payload)
	}
}

// Each service caps message length differently, and exceeding it is a rejection
// rather than a silent trim -- so the cap has to be applied per transport.
func TestEachTransportTruncatesToItsOwnLimit(t *testing.T) {
	long := strings.Repeat("a", 5000)

	cases := []struct {
		name  string
		limit int
		field string
		send  func(server *httptest.Server) types.NotificationOutcome
	}{
		{
			name:  "discord",
			limit: types.DiscordMaxMessageLength,
			field: "content",
			send: func(server *httptest.Server) types.NotificationOutcome {
				return NewDiscord(&DiscordSpec{WebhookURL: server.URL}, discard()).
					Send(context.Background(), Message{Body: long})
			},
		},
		{
			name:  "slack",
			limit: types.SlackMaxMessageLength,
			field: "text",
			send: func(server *httptest.Server) types.NotificationOutcome {
				return NewSlack(&SlackSpec{WebhookURL: server.URL}, discard()).
					Send(context.Background(), Message{Body: long})
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server, seen := capture(t)

			if outcome := testCase.send(server); !outcome.Delivered {
				t.Fatalf("outcome = %+v, want delivered", outcome)
			}

			payload := decode[map[string]string](t, seen.body)
			sent := []rune(payload[testCase.field])
			if len(sent) != testCase.limit {
				t.Fatalf("sent %d runes, want exactly the %d-rune limit", len(sent), testCase.limit)
			}
			if !strings.HasSuffix(string(sent), types.TruncationSuffix) {
				t.Fatal("a truncated message must say so, or it reads as the whole alert")
			}
		})
	}
}

func TestTheGenericWebhookSendsAStablePayloadAndHeaders(t *testing.T) {
	server, seen := capture(t)

	webhook := NewWebhook(&WebhookSpec{
		URL:     server.URL,
		Headers: map[string]string{"Authorization": "Bearer secret"},
	}, discard())

	outcome := webhook.Send(context.Background(), Message{Subject: "breckr: prices", Body: "it dropped"})
	if !outcome.Delivered {
		t.Fatalf("outcome = %+v, want delivered", outcome)
	}

	if seen.headers.Get("Authorization") != "Bearer secret" {
		t.Fatalf("Authorization = %q, want the configured header", seen.headers.Get("Authorization"))
	}
	if seen.headers.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", seen.headers.Get("Content-Type"))
	}

	payload := decode[map[string]any](t, seen.body)
	// Whatever is on the other end parses these names, so they are contract.
	for _, field := range []string{"subject", "message", "sent_at", "source"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("payload = %v, missing %q", payload, field)
		}
	}
	if payload["message"] != "it dropped" {
		t.Fatalf("message = %v, want the alert body", payload["message"])
	}
}

// PUT is accepted for receivers that treat an alert as an upsert; the default
// stays POST so the common case needs no config.
func TestTheGenericWebhookHonoursItsMethod(t *testing.T) {
	server, seen := capture(t)

	// Lowercase on purpose: the spec normalises it, so a config written by hand
	// works the same as one the dashboard produced.
	webhook := NewWebhook(&WebhookSpec{URL: server.URL, Method: "put"}, discard())
	webhook.Send(context.Background(), Message{Body: "hello"})

	if seen.method != http.MethodPut {
		t.Fatalf("method = %q, want PUT", seen.method)
	}
}
