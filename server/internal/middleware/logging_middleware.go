// Package middleware holds the cross-cutting HTTP handlers.
//
// There is deliberately no authentication here. Task specs are interpreted and
// never evaluated, and the security posture rests on binding to loopback and on
// every published Docker port being 127.0.0.1-scoped -- see the README.
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
