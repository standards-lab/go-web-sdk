package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"

	"github.com/standards-lab/go-web-sdk"
)

// Recoverer recovers a handler panic and logs it and its stack at error
// level. It then writes a 500 problem or, when the response is already
// committed, re-raises http.ErrAbortHandler. A panic with
// http.ErrAbortHandler itself passes through unlogged. It panics on a nil
// logger.
func Recoverer(logger *slog.Logger) web.Middleware {
	if logger == nil {
		panic("middleware: Recoverer requires a *slog.Logger")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := web.WrapWriter(w)

			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if p == http.ErrAbortHandler {
					panic(p)
				}

				base := appendRequestID([]slog.Attr{
					slog.String("http.request.method", r.Method),
					slog.String("url.path", r.URL.Path),
					slog.String("client.address", r.RemoteAddr),
				}, r)
				attrs := slices.Clip(base)
				if rec.Committed() {
					attrs = append(attrs, slog.Int("http.response.status_code", rec.Status()))
				}
				attrs = append(attrs,
					slog.Any("panic", p),
					slog.String("stack", string(debug.Stack())),
				)
				logger.LogAttrs(r.Context(), slog.LevelError, "handler panicked", attrs...)

				if rec.Committed() {
					panic(http.ErrAbortHandler)
				}
				err := web.WriteProblem(
					rec,
					r,
					http.StatusInternalServerError,
					"",
					"The server encountered an unexpected condition.",
				)
				if err != nil {
					logger.LogAttrs(r.Context(), slog.LevelError, "failed to write problem response",
						append(slices.Clip(base),
							slog.Int("http.response.status_code", rec.Status()),
							slog.String("error", err.Error()),
						)...,
					)
				}
			}()

			next.ServeHTTP(rec, r)
		})
	}
}
