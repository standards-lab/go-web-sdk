package handlertest

import (
	"net/http"
	"net/http/httptest"
)

// Serve serves one request of method for path through h, with no body, and
// returns the recorder.
func Serve(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// Get is [Serve] for a GET.
func Get(h http.Handler, path string) *httptest.ResponseRecorder {
	return Serve(h, http.MethodGet, path)
}
