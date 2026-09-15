package middleware_test

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// noContent is a handler that answers 204 and nothing else.
var noContent = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
})

func TestHeaders_SetsEveryEntry(t *testing.T) {
	handler := web.Chain(noContent, middleware.Headers(map[string]string{
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	}))

	rec := webtest.Probe(handler, "/orders/7")

	for name, want := range map[string]string{
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

// A name is canonicalized as http.Header.Set canonicalizes it, so a
// lowercase key lands on the same header a client reads by its canonical
// name.
func TestHeaders_CanonicalizesNames(t *testing.T) {
	handler := web.Chain(noContent, middleware.Headers(map[string]string{
		"cache-control": "no-store",
	}))

	rec := webtest.Probe(handler, "/orders/7")

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header()["Cache-Control"]; len(got) != 1 {
		t.Errorf("Cache-Control values = %v, want exactly one under the canonical key", got)
	}
	if n := len(rec.Header()); n != 1 {
		t.Errorf("response has %d headers, want the one canonical key only: %v", n, rec.Header())
	}
}

func TestHeaders_EmptyMapAppliesNothing(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			rec := webtest.Probe(web.Chain(noContent, middleware.Headers(headers)), "/orders/7")

			if rec.Code != http.StatusNoContent {
				t.Errorf("status = %d, want 204", rec.Code)
			}
			if len(rec.Header()) != 0 {
				t.Errorf("headers = %v, want none", rec.Header())
			}
		})
	}
}

// The headers are set before the handler runs, so they are on the response
// whoever writes it: here a Recoverer answering a panic, in either chain
// order.
func TestHeaders_SurviveARecoveredPanic(t *testing.T) {
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	fixed := middleware.Headers(map[string]string{"Cache-Control": "no-store"})
	tests := []struct {
		name    string
		handler http.Handler
	}{
		{"Headers outside Recoverer", web.Chain(panicking, fixed, middleware.Recoverer(logger))},
		{"Recoverer outside Headers", web.Chain(panicking, middleware.Recoverer(logger), fixed)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := webtest.Probe(tt.handler, "/orders/7")

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q on the recovered response, want no-store", got)
			}
		})
	}
}

// The middleware sets; it does not enforce. A handler that sets the same
// header afterwards wins.
func TestHeaders_HandlerCanOverride(t *testing.T) {
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "max-age=60")
		w.WriteHeader(http.StatusOK)
	}), middleware.Headers(map[string]string{"Cache-Control": "no-store"}))

	rec := webtest.Probe(handler, "/orders/7")

	if got := rec.Header().Get("Cache-Control"); got != "max-age=60" {
		t.Errorf("Cache-Control = %q, want the handler's max-age=60", got)
	}
}

// The map is copied at construction: a caller that mutates it afterwards
// changes nothing.
func TestHeaders_CopiesTheMap(t *testing.T) {
	headers := map[string]string{"Cache-Control": "no-store"}
	handler := web.Chain(noContent, middleware.Headers(headers))
	headers["Cache-Control"] = "public"
	headers["X-Added-Later"] = "yes"

	rec := webtest.Probe(handler, "/orders/7")

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want the value at construction, no-store", got)
	}
	if got := rec.Header().Get("X-Added-Later"); got != "" {
		t.Errorf("X-Added-Later = %q, want it absent", got)
	}
}

func TestHeaders_EmptyNamePanics(t *testing.T) {
	mustPanic(t, `Headers({"": ...})`, func() {
		middleware.Headers(map[string]string{"": "value"})
	})
}

// Two names that canonicalize to one header would be applied in map order,
// random per request; that is a wiring mistake, caught at construction.
func TestHeaders_DuplicateCanonicalNamePanics(t *testing.T) {
	mustPanic(t, `Headers({"cache-control", "Cache-Control"})`, func() {
		middleware.Headers(map[string]string{
			"cache-control": "no-store",
			"Cache-Control": "public",
		})
	})
}
