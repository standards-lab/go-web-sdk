package web

import "net/http"

// serveMux serves req through mux, substituting notFound and
// methodNotAllowed for ServeMux's own plain-text answers to a miss.
//
// ServeMux exposes no not-found hook, and a catch-all "/" pattern would
// defeat its 405 detection (every request would match some pattern), so the
// discrimination goes through [http.ServeMux.Handler] instead: a non-empty
// pattern is a real match; an empty one is a miss of some kind — a
// path-cleaning redirect, a 405, or a 404 — that ServeMux only
// distinguishes by what its built-in handler writes. That handler runs
// once into a recorder that discards the body, and the recorded status
// decides: a redirect is served on w as is (RedirectHandler is idempotent),
// a 405 keeps the recorded Allow header and goes to methodNotAllowed, and
// everything else is a 404.
//
// A match is served through [http.ServeMux.ServeHTTP], not the returned
// handler directly: Handler does not modify the request, and only ServeHTTP
// sets the fields behind [http.Request.Pattern] and
// [http.Request.PathValue]. The match runs twice on the hit path; that is
// the cost of the discrimination, since ServeMux exports no other way to
// populate those fields.
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

// missRecorder captures the status and header ServeMux's built-in miss
// handler writes and discards the body, so serveMux can tell a redirect from
// a 404 from a 405 before anything reaches the real writer.
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

// problemHandler answers every request with the status's undecorated
// problem document: about:blank, the status phrase as title, no detail, and
// the request path as instance. The encoder's error is dropped, as
// [Handle]'s is; nothing here logs.
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
