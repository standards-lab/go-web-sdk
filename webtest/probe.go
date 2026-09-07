package webtest

import (
	"net/http"
	"net/http/httptest"
)

// Probe serves one GET through h and returns the recorder.
func Probe(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}
