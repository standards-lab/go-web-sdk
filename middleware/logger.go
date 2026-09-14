package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

// RequestLogger emits one record per request, at info level, with the
// message "request" and these attributes, named by OpenTelemetry's semantic
// conventions where one exists:
//
//   - http.request.method: the request method.
//   - url.path: the request path.
//   - http.route: the matched route template, present only when the
//     request matched a ServeMux pattern (see below).
//   - http.response.status_code: the status the client got.
//   - duration: the time from entry to the handler's return, as a
//     [slog.Duration]. The conventions name no log attribute for it
//     (http.server.request.duration is a metric), so the name is the
//     SDK's own.
//   - client.address: the request's RemoteAddr.
//   - request_id: the correlation id from the request's context, present
//     only when one is set ([web.WithRequestID], as [RequestID] does).
//
// A successful request to [web.HealthPath] or [web.ReadyPath] logs at
// debug, keeping orchestrator heartbeat out of production logs while a
// failing probe stays visible.
//
// http.route is [http.Request.Pattern] with its method token removed
// ("GET /orders/{id}" logs as "/orders/{id}"; a pattern with no method is
// logged whole). ServeMux sets Pattern on the request it dispatches, and
// RequestLogger reads it back after the handler returns from the request
// it passed down, unchanged, so the two are the same request only when no
// middleware between RequestLogger and the mux forwards a derived request.
// [RequestID] forwards one (through [http.Request.WithContext]), so in
// Chain(mux, RequestLogger(l), RequestID()) the mux sets Pattern on
// RequestID's copy and RequestLogger's own request stays empty. Put
// RequestID, and any other middleware that derives a request, outside
// RequestLogger — Chain(mux, RequestID(), RequestLogger(l)) — which is
// also the order that puts the id in RequestLogger's context. Inside a
// route's own chain (group or per-route middleware) the pattern is already
// set when RequestLogger runs, and the order does not matter. A request
// that matched nothing, answered by a router's or module's miss handler,
// has no pattern; http.route is then omitted, never logged empty.
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
