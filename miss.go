package web

import "net/http"

// See docs/routing.md

func serveMux(
	w http.ResponseWriter,
	req *http.Request,
	mux *http.ServeMux,
	notFound, methodNotAllowed http.Handler,
) {
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

func problemHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Problem{Status: status}.WriteFor(w, r)
	})
}

var (
	defaultNotFound         = problemHandler(http.StatusNotFound)
	defaultMethodNotAllowed = problemHandler(http.StatusMethodNotAllowed)
)

func orDefault(h, def http.Handler) http.Handler {
	if h == nil {
		return def
	}
	return h
}
