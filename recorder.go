package web

import (
	"io"
	"net/http"
)

// Recorder wraps a ResponseWriter to record whether the response has been
// committed and with what status. The first final WriteHeader commits (a 1xx
// other than 101 is informational, as in net/http), and a Write, ReadFrom,
// or Flush before it commits an implicit 200. It implements Unwrap, so
// http.ResponseController reaches the writer beneath, and delegates
// io.ReaderFrom. It does not expose http.Pusher, and a Hijack through the
// controller bypasses it, as it bypasses net/http's own bookkeeping. [Handle]
// and the middleware package's RequestLogger and Recoverer share one per
// request through [WrapWriter].
type Recorder struct {
	http.ResponseWriter
	status    int
	committed bool
}

// WrapWriter returns w as a *Recorder, wrapping it only if it is not one
// already.
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
	final := code < 100 || code > 199 || code == http.StatusSwitchingProtocols
	if final && !rec.committed {
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

// FlushError flushes the writer beneath through http.ResponseController,
// which checks for this method before it unwraps, so the flush commits here.
func (rec *Recorder) FlushError() error {
	err := http.NewResponseController(rec.ResponseWriter).Flush()
	if err == nil && !rec.committed {
		rec.status = http.StatusOK
		rec.committed = true
	}
	return err
}

// Flush is [Recorder.FlushError] for a handler that asserts http.Flusher,
// with the error dropped as that interface requires.
func (rec *Recorder) Flush() {
	_ = rec.FlushError()
}
