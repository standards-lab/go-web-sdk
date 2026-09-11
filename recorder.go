package web

import (
	"io"
	"net/http"
)

// Recorder wraps a ResponseWriter to record whether a response has been
// committed and with what status. The first WriteHeader commits; a Write
// with no WriteHeader before it commits an implicit 200, which is the case
// a status-only recorder misses. It implements Unwrap so
// http.ResponseController reaches the underlying writer, and delegates
// io.ReaderFrom. Handle and the middleware package's RequestLogger and
// Recoverer all wrap through WrapWriter, so a request several of them see
// shares one Recorder instead of nesting.
type Recorder struct {
	http.ResponseWriter
	status    int
	committed bool
}

// WrapWriter returns w as a *Recorder, wrapping it if it is not one
// already. Wrapping is idempotent: a writer already wrapped comes back
// unchanged.
func WrapWriter(w http.ResponseWriter) *Recorder {
	if rec, ok := w.(*Recorder); ok {
		return rec
	}
	return &Recorder{ResponseWriter: w}
}

// Status reports the status committed so far, or 0 if nothing has been
// written yet.
func (rec *Recorder) Status() int {
	return rec.status
}

// Committed reports whether a status has been written yet.
func (rec *Recorder) Committed() bool {
	return rec.committed
}

func (rec *Recorder) WriteHeader(code int) {
	if !rec.committed {
		rec.status = code
		rec.committed = true
	}
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *Recorder) Write(b []byte) (int, error) {
	if !rec.committed {
		rec.status = http.StatusOK
		rec.committed = true
	}
	return rec.ResponseWriter.Write(b)
}

func (rec *Recorder) ReadFrom(src io.Reader) (int64, error) {
	if !rec.committed {
		rec.status = http.StatusOK
		rec.committed = true
	}
	if rf, ok := rec.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return io.Copy(rec.ResponseWriter, src)
}

func (rec *Recorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}
