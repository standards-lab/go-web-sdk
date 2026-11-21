package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

// See docs/middleware.md

// RequestLogger emits one info record per request, with the message
// "request" and the attributes http.request.method, url.path, http.route
// (the matched pattern without its method, when one matched),
// http.response.status_code (500 for a panic unwinding with nothing
// committed), duration, client.address, and request_id (when [RequestID] set
// one). A successful probe of [web.HealthPath] or [web.ReadyPath] logs at
// debug. Chain it after RequestID and any other middleware that derives a
// request, so the request it reads is the one carrying the id and the route.
// It panics on a nil logger.
func RequestLogger(logger *slog.Logger) web.Middleware {
	if logger == nil {
		panic("middleware: RequestLogger requires a *slog.Logger")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := web.WrapWriter(w)

			returnedWithoutPanic := false
			defer func() {
				status := rec.Status()
				switch {
				case rec.Committed():
				case returnedWithoutPanic:
					status = http.StatusOK
				default:
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
			returnedWithoutPanic = true
		})
	}
}

func routeOf(pattern string) string {
	if i := strings.IndexAny(pattern, " \t"); i >= 0 {
		return strings.TrimLeft(pattern[i+1:], " \t")
	}
	return pattern
}

func appendRequestID(attrs []slog.Attr, r *http.Request) []slog.Attr {
	if id, ok := web.RequestIDFrom(r.Context()); ok && id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}
	return attrs
}
