package api

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"breckr-server/internal/events"
	"breckr-server/internal/types"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Subscriber is the slice of the event bus the socket needs.
type Subscriber interface {
	Subscribe() (<-chan events.Event, func())
}

type EventsHandler struct {
	logger  *log.Logger
	bus     Subscriber
	origins []string
}

func NewEventsHandler(logger *log.Logger, bus Subscriber, allowedOrigins []string) *EventsHandler {
	return &EventsHandler{logger: logger, bus: bus, origins: originHosts(allowedOrigins)}
}

// HandleSubscribe streams change notifications for as long as the socket lives.
//
// One-directional by design: the client never sends anything, and every frame
// is a signal to refetch rather than the data itself. That is what keeps run
// filtering, pagination and totals server-side instead of duplicated into a
// second implementation on the client that can disagree with this one.
func (eh *EventsHandler) HandleSubscribe(w http.ResponseWriter, r *http.Request) {
	// A hijacked connection inherits the deadlines http.Server set for what it
	// assumed was an ordinary request, which would tear this socket down
	// mid-life. Frames get their own per-message deadline further down instead.
	control := http.NewResponseController(w)
	_ = control.SetReadDeadline(time.Time{})
	_ = control.SetWriteDeadline(time.Time{})

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: eh.origins,
	})
	if err != nil {
		// Accept has already answered the request, so there is nothing left to
		// write -- but a rejected handshake is worth saying out loud, because a
		// misconfigured CLIENT_ALLOWED_ORIGIN looks exactly like a dashboard
		// that silently never updates.
		eh.logger.Printf("WARN: rejected an events subscription: %v", err)
		return
	}
	defer conn.CloseNow()

	// Nothing is ever sent client -> server, but the read side still has to be
	// drained for close frames and pong replies to land. CloseRead does that.
	ctx := conn.CloseRead(r.Context())

	stream, unsubscribe := eh.bus.Subscribe()
	defer unsubscribe()

	ping := time.NewTicker(types.EventsPingInterval)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-stream:
			if !ok {
				return
			}
			if err := eh.send(ctx, conn, event); err != nil {
				return
			}

		case <-ping.C:
			if err := eh.ping(ctx, conn); err != nil {
				return
			}
		}
	}
}

func (eh *EventsHandler) send(ctx context.Context, conn *websocket.Conn, event events.Event) error {
	ctx, cancel := context.WithTimeout(ctx, types.EventsWriteTimeout)
	defer cancel()

	return wsjson.Write(ctx, conn, event)
}

// ping bounds its own wait rather than using the connection's context: Ping
// blocks until the pong arrives, and a peer that is gone but whose TCP
// connection has not noticed would otherwise stall event delivery indefinitely.
func (eh *EventsHandler) ping(ctx context.Context, conn *websocket.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, types.EventsWriteTimeout)
	defer cancel()

	return conn.Ping(ctx)
}

// originHosts turns the configured CORS origins into the host patterns the
// websocket handshake matches on.
//
// CLIENT_ALLOWED_ORIGIN holds full origins ("http://localhost:5173") because
// that is what the CORS middleware wants, while the handshake compares hosts
// alone -- so the scheme has to come off. This is load-bearing in development
// specifically: Vite proxies /api with changeOrigin, so the server sees Host
// 127.0.0.1:3000 against Origin localhost:5173 and the request does not qualify
// as same-origin. In production the dashboard is served from this same origin
// and is authorized without consulting this list at all.
func originHosts(origins []string) []string {
	hosts := make([]string, 0, len(origins))

	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		switch {
		case origin == "":
			continue
		case origin == "*":
			hosts = append(hosts, "*")
		default:
			if parsed, err := url.Parse(origin); err == nil && parsed.Host != "" {
				hosts = append(hosts, parsed.Host)
				continue
			}
			// Already a bare host, which is also a reasonable thing to configure.
			hosts = append(hosts, origin)
		}
	}

	return hosts
}
