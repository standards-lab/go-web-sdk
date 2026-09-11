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
// heartbeat out of production logs while a failing probe stays visible; a
// panicking handler logs at error with the panic value attached before the
// panic continues to net/http's recovery. The wrapped ResponseWriter records
// the first status written, implements Unwrap so http.ResponseController
// reaches through it, and delegates io.ReaderFrom.
func RequestLogger(logger *slog.Logger) web.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := web.WrapWriter(w)

			defer func() {
				// An uncommitted response still reaches the client as an
				// implicit 200: net/http sends one if the handler returns
				// (or panics past this deferred log) without writing
				// anything itself.
				status := rec.Status()
				if !rec.Committed() {
					status = http.StatusOK
				}

				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Duration("duration", time.Since(start)),
					slog.String("remote_addr", r.RemoteAddr),
				}
				if p := recover(); p != nil {
					attrs = append(attrs, slog.Any("panic", p))
					logger.LogAttrs(
						r.Context(),
						slog.LevelError,
						"request",
						attrs...,
					)
					panic(p)
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
					attrs...,
				)
			}()

			next.ServeHTTP(rec, r)
		})
	}
}
