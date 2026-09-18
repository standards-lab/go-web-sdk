package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// marking is a middleware whose one side effect is the X-Marked response
// header, so a test can tell whether it ran.
func marking(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Marked", "yes")
		next.ServeHTTP(w, r)
	})
}

// isWrite is a predicate holding for the request methods that carry a body.
func isWrite(r *http.Request) bool {
	return r.Method == http.MethodPost || r.Method == http.MethodPut
}

func TestMaybe_AppliesWhenThePredicateHolds(t *testing.T) {
	handler := web.Chain(noContent, middleware.Maybe(marking, func(*http.Request) bool { return true }))

	rec := webtest.Probe(handler, "/orders/7")

	if got := rec.Header().Get("X-Marked"); got != "yes" {
		t.Errorf("X-Marked = %q, want yes: the middleware did not run", got)
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

func TestMaybe_SkipsWhenThePredicateFails(t *testing.T) {
	handler := web.Chain(noContent, middleware.Maybe(marking, func(*http.Request) bool { return false }))

	rec := webtest.Probe(handler, "/orders/7")

	if _, present := rec.Header()["X-Marked"]; present {
		t.Errorf("X-Marked = %q, want it absent: the middleware ran", rec.Header().Get("X-Marked"))
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want the handler's 204", rec.Code)
	}
}

// The predicate decides per request, on the request it is given.
func TestMaybe_PredicateSeesEachRequest(t *testing.T) {
	handler := web.Chain(noContent, middleware.Maybe(marking, isWrite))
	tests := []struct {
		method string
		marked bool
	}{
		{http.MethodGet, false},
		{http.MethodPost, true},
		{http.MethodDelete, false},
		{http.MethodPut, true},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tt.method, "/orders/7", nil))

			if _, present := rec.Header()["X-Marked"]; present != tt.marked {
				t.Errorf("X-Marked present = %v, want %v", present, tt.marked)
			}
		})
	}
}

// Maybe composes mw around the next handler once, when the chain is built,
// never per request. A middleware that counts its own constructions (calls
// of the outer func(http.Handler) http.Handler) proves it: after one chain
// composition and several requests, both branches taken, the count is 1.
func TestMaybe_ComposesTheMiddlewareOnce(t *testing.T) {
	constructions := 0
	counting := func(next http.Handler) http.Handler {
		constructions++
		return marking(next)
	}
	handler := web.Chain(noContent, middleware.Maybe(counting, isWrite))

	if constructions != 1 {
		t.Fatalf("constructions after composing the chain = %d, want 1", constructions)
	}
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodGet} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/orders/7", nil))
	}
	if constructions != 1 {
		t.Errorf("constructions after five requests = %d, want still 1", constructions)
	}
}

// Under a group, composition happens at registration, once per route the
// group's chain applies to; the request path never constructs.
func TestMaybe_ComposesOncePerRegistrationUnderAGroup(t *testing.T) {
	constructions := 0
	counting := func(next http.Handler) http.Handler {
		constructions++
		return marking(next)
	}
	g := web.NewGroup("/api")
	g.Use(middleware.Maybe(counting, isWrite))
	g.Handle(http.MethodPost, "/orders", noContent)
	r := web.NewRouter()
	r.Mount(web.NewModule(g))

	before := constructions
	for range 3 {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/orders", nil))
	}

	if before == 0 {
		t.Fatal("the middleware was never composed at registration")
	}
	if constructions != before {
		t.Errorf("constructions = %d after three requests, want the %d from registration", constructions, before)
	}
}

func TestMaybe_NilMiddlewarePanics(t *testing.T) {
	mustPanic(t, "Maybe(nil, pred)", func() {
		middleware.Maybe(nil, isWrite)
	})
}

func TestMaybe_NilPredicatePanics(t *testing.T) {
	mustPanic(t, "Maybe(mw, nil)", func() {
		middleware.Maybe(marking, nil)
	})
}

func TestNotProbe(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{web.HealthPath, false},
		{web.ReadyPath, false},
		{"/orders/7", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if got := middleware.NotProbe(req); got != tt.want {
				t.Errorf("NotProbe(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// A middleware wrapped in Maybe(mw, NotProbe) never runs against either
// probe path, and does run against an ordinary route.
func TestMaybe_WithNotProbeExcludesBothProbes(t *testing.T) {
	handler := web.Chain(noContent, middleware.Maybe(marking, middleware.NotProbe))

	for _, path := range []string{web.HealthPath, web.ReadyPath} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if _, present := rec.Header()["X-Marked"]; present {
			t.Errorf("path %s: X-Marked present, want the probe excluded", path)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orders/7", nil))
	if got := rec.Header().Get("X-Marked"); got != "yes" {
		t.Errorf("X-Marked = %q, want yes: an ordinary route still runs the middleware", got)
	}
}
