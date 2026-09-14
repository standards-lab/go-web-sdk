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

// Handle adapts fn into an http.Handler: a returned error is written as a
// problem response through ew. The adapter never writes a second response.
// A handler that has already committed a response — written a status or a
// body — and then returns an error produces a logged failure, at error
// level through the writer's logger, and nothing on the wire. A problem
// whose body cannot be written (the encoder failed, typically because the
// client dropped the connection) is also logged at error level through the
// writer's logger. A nil writer panics: the adapter has no fallback policy,
// and a missing writer is a wiring mistake.
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
			ew.log().LogAttrs(
				r.Context(),
				slog.LevelError,
				"handler returned an error after committing its response",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.Status()),
				slog.String("error", err.Error()),
			)
			return
		}
		if werr := ew.Write(rec, r, err); werr != nil {
			ew.log().LogAttrs(
				r.Context(),
				slog.LevelError,
				"failed to write problem response",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.Status()),
				slog.String("error", werr.Error()),
			)
		}
	})
}
