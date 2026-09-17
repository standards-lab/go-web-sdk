package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware"
)

// send serves one request of the given method through handler, with the
// Content-Type header set when contentType is non-empty and absent
// otherwise, and returns the recorder.
func send(handler http.Handler, method, contentType string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/orders", strings.NewReader("{}"))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	handler.ServeHTTP(rec, req)
	return rec
}

// problem415 fails the test unless rec holds a 415 problem document, and
// returns the document.
func problem415(t *testing.T, rec *httptest.ResponseRecorder) web.Problem {
	t.Helper()
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != web.ProblemMediaType {
		t.Errorf("Content-Type = %q, want %q", got, web.ProblemMediaType)
	}
	var p web.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.Status != http.StatusUnsupportedMediaType {
		t.Errorf("problem status = %d, want 415", p.Status)
	}
	if p.Type != web.ProblemTypeBlank {
		t.Errorf("problem type = %q, want %q", p.Type, web.ProblemTypeBlank)
	}
	if p.Title != http.StatusText(http.StatusUnsupportedMediaType) {
		t.Errorf("problem title = %q, want the status phrase", p.Title)
	}
	if p.Instance != "/orders" {
		t.Errorf("problem instance = %q, want /orders", p.Instance)
	}
	return p
}

func TestContentType_AllowedTypePasses(t *testing.T) {
	handler := web.Chain(noContent, middleware.ContentType("application/json", "application/merge-patch+json"))

	for _, ct := range []string{"application/json", "application/merge-patch+json"} {
		t.Run(ct, func(t *testing.T) {
			rec := send(handler, http.MethodPost, ct)

			if rec.Code != http.StatusNoContent {
				t.Errorf("status = %d, want the handler's 204", rec.Code)
			}
		})
	}
}

// A disallowed type is a 415 problem document whose detail names the type
// received and the types accepted.
func TestContentType_DisallowedTypeIsA415(t *testing.T) {
	handler := web.Chain(noContent, middleware.ContentType("application/json"))

	p := problem415(t, send(handler, http.MethodPost, "text/xml"))

	if !strings.Contains(p.Detail, "text/xml") {
		t.Errorf("detail = %q, want it to name the received type", p.Detail)
	}
	if !strings.Contains(p.Detail, "application/json") {
		t.Errorf("detail = %q, want it to name the accepted type", p.Detail)
	}
}

// Only the media type is compared: a parameter does not defeat the match,
// and neither does case.
func TestContentType_ParametersAndCaseDoNotMatter(t *testing.T) {
	handler := web.Chain(noContent, middleware.ContentType("application/json"))

	for _, ct := range []string{
		"application/json; charset=utf-8",
		"application/json;charset=UTF-8",
		"Application/JSON",
		"APPLICATION/JSON; Charset=utf-8",
	} {
		t.Run(ct, func(t *testing.T) {
			rec := send(handler, http.MethodPost, ct)

			if rec.Code != http.StatusNoContent {
				t.Errorf("status = %d, want 204: %q did not match application/json", rec.Code, ct)
			}
		})
	}
}

// The allowlist is normalized too, so the wiring may spell a type as it
// likes.
func TestContentType_AllowedTypeIsNormalized(t *testing.T) {
	handler := web.Chain(noContent, middleware.ContentType(" Application/JSON "))

	rec := send(handler, http.MethodPost, "application/json")

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

func TestContentType_MissingHeaderIsA415(t *testing.T) {
	handler := web.Chain(noContent, middleware.ContentType("application/json"))

	p := problem415(t, send(handler, http.MethodPost, ""))

	if !strings.Contains(p.Detail, "no Content-Type header") {
		t.Errorf("detail = %q, want it to say the header is missing", p.Detail)
	}
}

// A header mime.ParseMediaType rejects is not in the allowlist, and the
// detail describes it without echoing it.
func TestContentType_MalformedHeaderIsA415(t *testing.T) {
	handler := web.Chain(noContent, middleware.ContentType("application/json"))

	for _, ct := range []string{"application/", "application/json; charset", "text/plain;;", "/json"} {
		t.Run(ct, func(t *testing.T) {
			p := problem415(t, send(handler, http.MethodPost, ct))

			// The detail is "<why>; accepted: <types>"; the accepted list
			// legitimately contains fragments like "application/" and
			// "/json", so only the why clause is checked for an echo.
			why, _, found := strings.Cut(p.Detail, "; accepted: ")
			if !found {
				t.Fatalf("detail = %q, want the why; accepted: types form", p.Detail)
			}
			if !strings.Contains(why, "not a media type") {
				t.Errorf("detail = %q, want it to say the header is not a media type", p.Detail)
			}
			if strings.Contains(why, ct) {
				t.Errorf("detail = %q echoes the malformed header", p.Detail)
			}
		})
	}
}

// The gate is method-agnostic on its own; scoping it to the requests that
// carry a body is Maybe's job. Under Maybe, writes are judged by their
// Content-Type and reads pass whatever they carry, including nothing.
func TestContentType_ScopedToWritesThroughMaybe(t *testing.T) {
	isWrite := func(r *http.Request) bool {
		return r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch
	}
	handler := web.Chain(noContent, middleware.Maybe(middleware.ContentType("application/json"), isWrite))
	tests := []struct {
		method, contentType string
		status              int
	}{
		{http.MethodPost, "application/json", http.StatusNoContent},
		{http.MethodPut, "application/json; charset=utf-8", http.StatusNoContent},
		{http.MethodPatch, "text/xml", http.StatusUnsupportedMediaType},
		{http.MethodPost, "", http.StatusUnsupportedMediaType},
		{http.MethodGet, "", http.StatusNoContent},
		{http.MethodGet, "text/xml", http.StatusNoContent},
		{http.MethodDelete, "", http.StatusNoContent},
	}
	for _, tt := range tests {
		name := tt.method + " " + tt.contentType
		if tt.contentType == "" {
			name = tt.method + " without Content-Type"
		}
		t.Run(name, func(t *testing.T) {
			rec := send(handler, tt.method, tt.contentType)

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

// Unscoped, the same gate rejects a GET without a Content-Type: the
// middleware does not know about methods.
func TestContentType_UnscopedGateRejectsABodylessRead(t *testing.T) {
	handler := web.Chain(noContent, middleware.ContentType("application/json"))

	_ = problem415(t, send(handler, http.MethodGet, ""))
}

func TestContentType_NoAllowedTypesPanics(t *testing.T) {
	mustPanic(t, "ContentType()", func() {
		middleware.ContentType()
	})
}

// An allowed entry that is not a bare type/subtype could never match, or
// would match something other than what it says; both are wiring mistakes.
func TestContentType_MalformedAllowedTypePanics(t *testing.T) {
	for _, a := range []string{"", "json", "application/", "application/json; charset=utf-8"} {
		mustPanic(t, "ContentType("+strings.TrimSpace(a)+")", func() {
			middleware.ContentType("application/json", a)
		})
	}
}
