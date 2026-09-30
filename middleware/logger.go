package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

// RequestLogger emits one info record per request, "request", with
// http.request.method, url.path, http.route (the matched pattern without its
// method, when one matched), http.response.status_code (500 for a panic
// unwinding with nothing committed), duration, client.address, and
// request_id (when [RequestID] set one). A successful probe of
// [web.HealthPath] or [web.ReadyPath] logs at debug. Chain it after RequestID
// and any other middleware that derives a request, so the request it reads
// is the one carrying the id and the route. It panics on a nil logger.
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

				attrs := make([]slog.Attr, 0, 7)
				attrs = append(attrs,
					slog.String("http.request.method", r.Method),
					slog.String("url.path", r.URL.Path),
				)
				if route := routeOf(r.Pattern); route != "" {
					attrs = append(attrs, slog.String("http.route", route))
				}
				attrs = append(attrs,
					slog.Int("http.response.status_code", status),
					slog.Duration("duration", time.Since(start)),
					slog.String("client.address", r.RemoteAddr),
				)
				attrs = appendRequestID(attrs, r)
				logger.LogAttrs(r.Context(), level, "request", attrs...)
			}()

			next.ServeHTTP(rec, r)
			returned = true
		})
	}
}

// routeOf returns the OpenTelemetry http.route for a ServeMux pattern: the
// pattern with its leading method token removed. ServeMux's pattern grammar
// is [METHOD ][HOST]/[PATH], the method ending at the first space or tab
// with any further blanks skipped, and this mirrors that split. A pattern
// with no method token is returned whole; an empty pattern stays empty.
func routeOf(pattern string) string {
	if i := strings.IndexAny(pattern, " \t"); i >= 0 {
		return strings.TrimLeft(pattern[i+1:], " \t")
	}
	return pattern
}

// appendRequestID appends a request_id attribute to attrs when r's context
// carries a non-empty correlation id, and returns attrs unchanged otherwise:
// a chain with no [RequestID] must not log a spurious empty id.
func appendRequestID(attrs []slog.Attr, r *http.Request) []slog.Attr {
	if id, ok := web.RequestIDFrom(r.Context()); ok && id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}
	return attrs
}
