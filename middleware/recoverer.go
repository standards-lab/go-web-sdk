package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/standards-lab/go-web-sdk"
)

// Recoverer turns a panicking handler into a 500 problem response. It is
// the chain's one recovery point: it recovers the panic and logs it at
// error level with the panic value and the goroutine stack through logger.
// If the handler had not committed a response, it then writes a 500
// problem document; the panic value stays out of it, in the log only, and
// the problem's detail is a fixed message. If that document cannot be
// written (the encoder failed, typically because the client dropped the
// connection), the failure is logged at error level as a second record.
//
// Both records describe the request by OpenTelemetry's semantic-convention
// names: http.request.method, url.path, and client.address, plus
// http.response.status_code where a status was committed (the panic record
// omits it when nothing was), and request_id when the request's context
// carries a correlation id ([web.WithRequestID], as [RequestID] does).
//
// A response already committed before the panic (a status or a body
// already written) cannot be answered with a problem document — matching
// [web.Handle]'s never-write-a-second-response discipline — so after
// logging, Recoverer re-raises the panic as [http.ErrAbortHandler]. That is
// net/http's own signal to end the connection without a further write: the
// same outcome an uncaught panic mid-response produced before Recoverer
// existed, rather than a truncated body completed as if it were a clean
// 200. The record still carries the committed status, from whichever
// middleware logs it.
//
// A panic that is already [http.ErrAbortHandler] is re-raised untouched
// without logging, so a handler that deliberately aborts a connection
// keeps net/http's silent handling.
//
// Recoverer wraps the ResponseWriter through [web.WrapWriter], so it shares
// one [web.Recorder] with [RequestLogger] and [web.Handle] in either chain
// order.
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

				attrs := []slog.Attr{
					slog.String("http.request.method", r.Method),
					slog.String("url.path", r.URL.Path),
					slog.String("client.address", r.RemoteAddr),
				}
				if rec.Committed() {
					attrs = append(attrs, slog.Int("http.response.status_code", rec.Status()))
				}
				attrs = appendRequestID(attrs, r)
				attrs = append(attrs,
					slog.Any("panic", p),
					slog.String("stack", string(debug.Stack())),
				)
				logger.LogAttrs(
					r.Context(),
					slog.LevelError,
					"handler panicked",
					attrs...,
				)

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
					attrs := []slog.Attr{
						slog.String("http.request.method", r.Method),
						slog.String("url.path", r.URL.Path),
						slog.String("client.address", r.RemoteAddr),
						slog.Int("http.response.status_code", rec.Status()),
					}
					attrs = appendRequestID(attrs, r)
					attrs = append(attrs, slog.String("error", err.Error()))
					logger.LogAttrs(
						r.Context(),
						slog.LevelError,
						"failed to write problem response",
						attrs...,
					)
				}
			}()

			next.ServeHTTP(rec, r)
		})
	}
}
