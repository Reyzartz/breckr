package middleware

import (
	"net/http"

	"breckr-server/internal/auth"
	"breckr-server/internal/utils"
)

type AuthMiddleware struct {
	sessions *auth.Sessions
}

func NewAuthMiddleware(sessions *auth.Sessions) *AuthMiddleware {
	return &AuthMiddleware{sessions: sessions}
}

// Annotate records whether this request is authenticated, and never rejects.
//
// Split from RequireAuth because /api/health has to answer an unauthenticated
// caller -- Docker's healthcheck cannot log in -- while still knowing not to
// hand that caller the browser endpoint and the task count.
//
// With no password configured, Sessions.Authenticated reports true for
// everything, so there is one path through here rather than an auth-enabled and
// an auth-disabled one to keep in agreement.
func (m *AuthMiddleware) Annotate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithAuthenticated(r.Context(), m.sessions.Authenticated(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth rejects a request Annotate did not authenticate.
//
// Note that neither method wraps the ResponseWriter, only the Request. /api/events
// hijacks the connection and clears the deadlines http.Server set for it, and
// both are found by following Unwrap -- see the comment on statusRecorder, which
// is the one wrapper in this package that has to implement it. A second wrapper
// without Unwrap would break the websocket rather than the log.
func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.IsAuthenticated(r.Context()) {
			// The same {error} envelope every other failure uses, so the
			// dashboard's axios interceptor parses a 401 without a special case.
			utils.WriteError(w, http.StatusUnauthorized, "Not signed in.", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}
