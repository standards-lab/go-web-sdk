package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

// RequestLogger emits one record per request: the method, path, status,
// duration, and remote address, at info level. A successful request to
// [web.HealthPath] or [web.ReadyPath] logs at debug, keeping orchestrator
// heartbeat out of production logs while a failing probe stays visible.
//
// The logger does not recover panics; that is [Recoverer]'s job, in either
// chain order. When a panic unwinds through the logger with no response
// committed, the record's status is 500: that is what a Recoverer outside
// the logger goes on to write, and without one net/http drops the
// connection, which a client experiences as a server failure. The panic
// value itself is logged by the Recoverer, or by net/http's own recovery
// when none is wired.
//
// The wrapped ResponseWriter records the first status written, implements
// Unwrap so http.ResponseController reaches through it, and delegates
// io.ReaderFrom.
func RequestLogger(logger *slog.Logger) web.Middleware {
	if logger == nil {
		panic("middleware: RequestLogger requires a *slog.Logger")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := web.WrapWriter(w)

			// returned is set only when the handler returns normally, so
			// the deferred log can tell a panic unwinding through it from
			// a handler that finished without writing, without calling
			// recover and becoming a second recovery point.
			returned := false
			defer func() {
				status := rec.Status()
				switch {
				case rec.Committed():
					// The client got this status, whether the handler
					// returned or panicked afterwards.
				case returned:
					// A handler that returns without writing gets an
					// implicit 200 from net/http.
					status = http.StatusOK
				default:
					// A panic is unwinding with nothing on the wire. A
					// Recoverer outside this logger writes a 500 problem
					// next; with none wired, net/http drops the
					// connection, and 500 is the honest label for what
					// the client saw.
					status = http.StatusInternalServerError
				}

				level := slog.LevelInfo
				probe := r.URL.Path == web.HealthPath ||
					r.URL.Path == web.ReadyPath
				if probe && status >= 200 && status < 300 {
					level = slog.LevelDebug
				}
				logger.LogAttrs(
					r.Context(),
					level,
					"request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Duration("duration", time.Since(start)),
					slog.String("remote_addr", r.RemoteAddr),
				)
			}()

			next.ServeHTTP(rec, r)
			returned = true
		})
	}
}
