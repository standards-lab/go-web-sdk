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

func send(handler http.Handler, method, contentType string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/orders", strings.NewReader("{}"))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	handler.ServeHTTP(rec, req)
	return rec
}

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

func TestContentType_MalformedHeaderIsA415(t *testing.T) {
	handler := web.Chain(noContent, middleware.ContentType("application/json"))

	for _, ct := range []string{"application/", "application/json; charset", "text/plain;;", "/json"} {
		t.Run(ct, func(t *testing.T) {
			p := problem415(t, send(handler, http.MethodPost, ct))

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

func TestContentType_UnscopedGateRejectsABodylessRead(t *testing.T) {
	handler := web.Chain(noContent, middleware.ContentType("application/json"))

	_ = problem415(t, send(handler, http.MethodGet, ""))
}

func TestContentType_NoAllowedTypesPanics(t *testing.T) {
	mustPanic(t, "ContentType()", func() {
		middleware.ContentType()
	})
}

func TestContentType_MalformedAllowedTypePanics(t *testing.T) {
	for _, a := range []string{"", "json", "application/", "application/json; charset=utf-8"} {
		mustPanic(t, "ContentType("+strings.TrimSpace(a)+")", func() {
			middleware.ContentType("application/json", a)
		})
	}
}
