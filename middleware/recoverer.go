package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/standards-lab/go-web-sdk"
)

// Recoverer turns a panicking handler into a 500 problem response. It is
// the chain's one recovery point: it recovers the panic, logs it at error
// level with the panic value and the goroutine stack through logger, and,
// if the handler had not committed a response, writes a 500 problem
// document. A response committed before the panic (a status or a body
// already written) is left alone, matching [web.Handle]'s never-write-a-
// second-response discipline: the client gets whatever was sent, and the
// record carries the committed status. The panic value stays in the log;
// the problem's detail is a fixed message.
//
// A panic with [http.ErrAbortHandler] is re-raised untouched, so a handler
// that deliberately aborts a connection keeps net/http's silent handling.
//
// Recoverer wraps the ResponseWriter through [web.WrapWriter], so it shares
// one [web.Recorder] with [RequestLogger] and [web.Handle] in either chain
// order.
func Recoverer(logger *slog.Logger) web.Middleware {
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

				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("remote_addr", r.RemoteAddr),
					slog.Any("panic", p),
					slog.String("stack", string(debug.Stack())),
				}
				if rec.Committed() {
					attrs = append(attrs, slog.Int("status", rec.Status()))
				}
				logger.LogAttrs(
					r.Context(),
					slog.LevelError,
					"handler panicked",
					attrs...,
				)

				if rec.Committed() {
					return
				}
				_ = web.WriteProblem(
					rec,
					r,
					http.StatusInternalServerError,
					"",
					"The server encountered an unexpected condition.",
				)
			}()

			next.ServeHTTP(rec, r)
		})
	}
}
