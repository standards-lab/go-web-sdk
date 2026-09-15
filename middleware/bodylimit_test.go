package middleware_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware"
)

type order struct {
	Name string `json:"name"`
}

// post serves one POST of body through handler and returns the recorder.
func post(handler http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	return rec
}

// A body under the limit reaches the handler whole.
func TestBodyLimit_BodyUnderTheLimitPassesThrough(t *testing.T) {
	var got string
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll: %v", err)
		}
		got = string(b)
		w.WriteHeader(http.StatusNoContent)
	}), middleware.BodyLimit(64))

	rec := post(handler, `{"name": "widget"}`)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if got != `{"name": "widget"}` {
		t.Errorf("handler read %q, want the whole body", got)
	}
}

// The composition the middleware exists for: a handler adapted by web.Handle
// that decodes through web.DecodeJSON answers an oversized body with a 413
// problem document, with no mapping of its own. The middleware's limit is
// the tighter one here, so it is the middleware's reader that overflows.
func TestBodyLimit_OverflowThroughDecodeJSONIsA413(t *testing.T) {
	decoded := false
	handler := web.Chain(web.Handle(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := web.DecodeJSON[order](w, r, 1<<10); err != nil {
			return err
		}
		decoded = true
		w.WriteHeader(http.StatusNoContent)
		return nil
	}, web.NewErrorWriter()), middleware.BodyLimit(16))

	rec := post(handler, `{"name": "`+strings.Repeat("x", 64)+`"}`)

	if decoded {
		t.Error("the handler decoded a body over the limit")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != web.ProblemMediaType {
		t.Errorf("Content-Type = %q, want %q", got, web.ProblemMediaType)
	}
	var p web.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("problem status = %d, want 413", p.Status)
	}
	if p.Type != web.ProblemTypeBlank {
		t.Errorf("problem type = %q, want %q", p.Type, web.ProblemTypeBlank)
	}
	if p.Instance != "/orders" {
		t.Errorf("problem instance = %q, want /orders", p.Instance)
	}
	if !strings.HasPrefix(p.Detail, "body: ") {
		t.Errorf("problem detail = %q, want DecodeJSON's body: reason", p.Detail)
	}
}

// A handler that reads the body itself gets the reader's own error and owns
// the mapping; the middleware writes nothing.
func TestBodyLimit_OverflowOnARawReadIsMaxBytesError(t *testing.T) {
	var err error
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}), middleware.BodyLimit(16))

	rec := post(handler, strings.Repeat("x", 64))

	if _, ok := errors.AsType[*http.MaxBytesError](err); !ok {
		t.Errorf("read error = %v, want *http.MaxBytesError", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want the handler's own 204", rec.Code)
	}
}

func TestBodyLimit_NonPositiveLimitPanics(t *testing.T) {
	for _, n := range []int64{0, -1} {
		mustPanic(t, "BodyLimit("+strconv.FormatInt(n, 10)+")", func() {
			middleware.BodyLimit(n)
		})
	}
}
