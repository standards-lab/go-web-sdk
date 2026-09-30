package web

import (
	"log/slog"
	"net/http"
)

// HandlerFunc is an http.HandlerFunc that reports failure by returning an
// error instead of writing it. A nil return means the handler wrote the
// response itself; a non-nil return is written as a problem by the
// [ErrorWriter] the handler is adapted with, so the handler's body reads as
// its success path and every rejection is one return statement.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts fn into an http.Handler that writes a returned error through
// ew, never as a second response: an error after commit, or a problem the
// encoder fails to write, is logged through ew's logger. It panics on a nil
// ew.
func Handle(fn HandlerFunc, ew *ErrorWriter) http.Handler {
	if ew == nil {
		panic("web: Handle requires an ErrorWriter; wire one with NewErrorWriter")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := WrapWriter(w)
		err := fn(rec, r)
		if err == nil {
			return
		}
		if rec.Committed() {
			ew.logger.LogAttrs(
				r.Context(),
				slog.LevelError,
				"handler returned an error after committing its response",
				handleAttrs(r, rec.Status(), err)...,
			)
			return
		}
		if werr := ew.Write(rec, r, err); werr != nil {
			ew.logger.LogAttrs(
				r.Context(),
				slog.LevelError,
				"failed to write problem response",
				handleAttrs(r, rec.Status(), werr)...,
			)
		}
	})
}

// handleAttrs is the attribute set of the error writer's records, named as
// the middleware package's request logger names them.
func handleAttrs(r *http.Request, status int, err error) []slog.Attr {
	attrs := make([]slog.Attr, 0, 6)
	attrs = append(attrs,
		slog.String("http.request.method", r.Method),
		slog.String("url.path", r.URL.Path),
		slog.String("client.address", r.RemoteAddr),
		slog.Int("http.response.status_code", status),
	)
	if id, ok := RequestIDFrom(r.Context()); ok && id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}
	return append(attrs, slog.String("error", err.Error()))
}
