package web

import (
	"io"
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
// level through the writer's logger, and nothing on the wire. A nil writer
// panics: the adapter has no fallback policy, and a missing writer is a
// wiring mistake.
func Handle(fn HandlerFunc, ew *ErrorWriter) http.Handler {
	if ew == nil {
		panic("web: Handle requires an ErrorWriter; wire one with NewErrorWriter")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &recorder{ResponseWriter: w}
		err := fn(rec, r)
		if err == nil {
			return
		}
		if rec.committed {
			ew.log().LogAttrs(
				r.Context(),
				slog.LevelError,
				"handler returned an error after committing its response",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.String("error", err.Error()),
			)
			return
		}
		_ = ew.Write(rec, r, err)
	})
}

// recorder wraps a ResponseWriter to record whether a response has been
// committed and with what status. The first WriteHeader commits; a Write
// with no WriteHeader before it commits an implicit 200, which is the case a
// status-only recorder misses. It implements Unwrap so http.ResponseController
// reaches the underlying writer, and delegates io.ReaderFrom.
type recorder struct {
	http.ResponseWriter
	status    int
	committed bool
}

func (rec *recorder) WriteHeader(code int) {
	if !rec.committed {
		rec.status = code
		rec.committed = true
	}
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *recorder) Write(b []byte) (int, error) {
	if !rec.committed {
		rec.status = http.StatusOK
		rec.committed = true
	}
	return rec.ResponseWriter.Write(b)
}

func (rec *recorder) ReadFrom(src io.Reader) (int64, error) {
	if !rec.committed {
		rec.status = http.StatusOK
		rec.committed = true
	}
	if rf, ok := rec.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return io.Copy(rec.ResponseWriter, src)
}

func (rec *recorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}
