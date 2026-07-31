// Package middleware holds the cross-cutting HTTP handlers.
//
// Authentication is optional and off by default: unset AUTH_PASSWORD and every
// request is authenticated, which is the right posture for a deployment
// reachable only from loopback -- which is still what the compose file
// publishes. Setting it is what makes exposing the port beyond this machine a
// reasonable thing to do.
//
// Neither is the whole story on its own. Task specs are interpreted and never
// evaluated, so nothing anyone types becomes code even after they are past the
// login -- see the executor.
package middleware

import (
	"log"
	"net/http"
	"time"
)

type LoggingMiddleware struct {
	logger *log.Logger
}

func NewLoggingMiddleware(logger *log.Logger) *LoggingMiddleware {
	return &LoggingMiddleware{logger: logger}
}

// statusRecorder captures the status code, which http.ResponseWriter does not
// expose after the fact.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

// Unwrap exposes the writer underneath.
//
// Embedding the interface forwards Write and Header but hides everything the
// concrete writer can also do, because a type assertion sees only this struct.
// /api/events needs two of those: hijacking the connection, and clearing the
// deadlines http.Server set for what it took to be an ordinary request. Both
// http.NewResponseController and the websocket handshake find them by following
// Unwrap, so this one method is what makes a long-lived connection possible
// through the log.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

func (m *LoggingMiddleware) LogRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		// Defaults to 200: a handler that writes a body without calling
		// WriteHeader never reaches the recorder.
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		m.logger.Printf("%s %s %d %s",
			r.Method, r.URL.Path, recorder.status, time.Since(started).Round(time.Millisecond))
	})
}
