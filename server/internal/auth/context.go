package auth

import (
	"context"
	"net"
	"net/http"
)

type contextKey struct{}

// authenticatedKey carries the verdict the auth middleware reached, so a handler
// downstream can vary its answer without re-reading the cookie -- /api/health
// is public but reports less to an anonymous caller.
var authenticatedKey = contextKey{}

// WithAuthenticated records the verdict for this request.
func WithAuthenticated(ctx context.Context, authenticated bool) context.Context {
	return context.WithValue(ctx, authenticatedKey, authenticated)
}

// IsAuthenticated reports the verdict the middleware recorded.
//
// A request that never passed through the middleware reads as unauthenticated,
// so a route accidentally registered outside it fails closed.
func IsAuthenticated(ctx context.Context) bool {
	authenticated, ok := ctx.Value(authenticatedKey).(bool)
	return ok && authenticated
}

// ClientKey identifies the caller for rate limiting.
//
// Deliberately RemoteAddr and never X-Forwarded-For: that header is set by the
// client on a direct connection, so keying on it would let anyone reset their
// own limit by changing one string. Behind a reverse proxy this collapses to a
// single key and the window becomes effectively global -- stricter rather than
// looser, but it does mean one attacker can lock out the legitimate user, which
// is why an internet-exposed deployment should rate-limit at the proxy too.
func ClientKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
