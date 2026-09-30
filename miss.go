package web

import "net/http"

// serveMux serves req through mux, with notFound and methodNotAllowed in
// place of ServeMux's plain-text misses. ServeMux has no not-found hook, and
// a catch-all pattern would defeat its 405, so a miss (an empty pattern from
// Handler) runs ServeMux's own handler into a recorder, and the recorded
// status picks the answer: a redirect as is, a 405 with its Allow, else a
// 404. A match is served through ServeHTTP, the only way to set
// [http.Request.Pattern] and PathValue, so the hit path matches twice.
func serveMux(
	w http.ResponseWriter,
	req *http.Request,
	mux *http.ServeMux,
	notFound, methodNotAllowed http.Handler,
) {
	// Mirrors ServeMux.ServeHTTP's own guard, which Handler skips.
	if req.RequestURI == "*" {
		if req.ProtoAtLeast(1, 1) {
			w.Header().Set("Connection", "close")
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	h, pattern := mux.Handler(req)
	if pattern != "" {
		mux.ServeHTTP(w, req)
		return
	}

	var rec missRecorder
	h.ServeHTTP(&rec, req)
	switch rec.status {
	case http.StatusMovedPermanently,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		h.ServeHTTP(w, req)
	case http.StatusMethodNotAllowed:
		if allow := rec.header.Get("Allow"); allow != "" {
			w.Header().Set("Allow", allow)
		}
		methodNotAllowed.ServeHTTP(w, req)
	default:
		notFound.ServeHTTP(w, req)
	}
}

// missRecorder records the status and header of ServeMux's miss handler
// and discards its body.
type missRecorder struct {
	header http.Header
	status int
}

func (m *missRecorder) Header() http.Header {
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}

func (m *missRecorder) WriteHeader(status int) {
	if m.status == 0 {
		m.status = status
	}
}

func (m *missRecorder) Write(b []byte) (int, error) {
	m.WriteHeader(http.StatusOK)
	return len(b), nil
}

// problemHandler answers with the status's undecorated problem. The
// encoder's error is dropped; nothing here logs.
func problemHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Problem{Status: status}.WriteFor(w, r)
	})
}

var (
	defaultNotFound         = problemHandler(http.StatusNotFound)
	defaultMethodNotAllowed = problemHandler(http.StatusMethodNotAllowed)
)

// orDefault returns h, or def when h is nil.
func orDefault(h, def http.Handler) http.Handler {
	if h == nil {
		return def
	}
	return h
}
