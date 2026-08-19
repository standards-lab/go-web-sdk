package web_test

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/internal/webtest"
)

// Every route registers under its full pattern: the parent's prefix, the
// child's, and the route's own, joined at compile time.
func TestNewModule_RegistersFullPatterns(t *testing.T) {
	child := web.NewGroup("/orders")
	child.Handle(http.MethodGet, "/recent", ok())

	root := web.NewGroup("/api/v1")
	root.Handle(http.MethodGet, "/status", ok())
	root.Mount(child)

	m := web.NewModule(root)
	for _, path := range []string{"/api/v1/status", "/api/v1/orders/recent"} {
		if got := webtest.Probe(m, path).Code; got != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, got)
		}
	}
	if got := webtest.Probe(m, "/orders/recent").Code; got != http.StatusNotFound {
		t.Errorf("GET /orders/recent = %d, want 404 without the full prefix", got)
	}
}

// Group middleware wraps outermost, ordered root to leaf, with per-route
// middleware innermost — the effective order a request passes through.
func TestNewModule_MiddlewareOrderIsRootToLeafThenRoute(t *testing.T) {
	var order []string

	child := web.NewGroup("/orders")
	child.Use(tag(&order, "leaf"))
	child.Handle(http.MethodGet, "/recent", http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			order = append(order, "handler")
		}), tag(&order, "route"))

	root := web.NewGroup("/api")
	root.Use(tag(&order, "root"))
	root.Mount(child)

	webtest.Probe(web.NewModule(root), "/api/orders/recent")

	want := []string{"root", "leaf", "route", "handler"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// Composition happens once, at NewModule: the middleware's wrapping function
// runs at compile time and never again per request.
func TestNewModule_ComposesOnce(t *testing.T) {
	var composed int
	counting := func(next http.Handler) http.Handler {
		composed++
		return next
	}

	g := web.NewGroup("/api")
	g.Use(counting)
	g.Handle(http.MethodGet, "/things", ok())
	m := web.NewModule(g)

	webtest.Probe(m, "/api/things")
	webtest.Probe(m, "/api/things")

	if composed != 1 {
		t.Errorf("middleware composed %d times, want once at NewModule", composed)
	}
}

func TestNewModule_DuplicatePatternPanics(t *testing.T) {
	g := web.NewGroup("/api")
	g.Handle(http.MethodGet, "/things", ok())
	g.Handle(http.MethodGet, "/things", ok())

	mustPanic(t, "NewModule with a duplicate pattern", func() {
		web.NewModule(g)
	})
}

// NewHandlerModule is the one place a module rewrites the request path; a
// compiled group's handler sees the full path unchanged.
func TestNewHandlerModule_StripsThePrefix(t *testing.T) {
	var stripped, full string

	hm := web.NewHandlerModule("/app", http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			stripped = r.URL.Path
		}))
	webtest.Probe(hm, "/app/index.html")
	if stripped != "/index.html" {
		t.Errorf("handler module saw %q, want the prefix stripped", stripped)
	}

	g := web.NewGroup("/api")
	g.Handle(http.MethodGet, "/things", http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			full = r.URL.Path
		}))
	webtest.Probe(web.NewModule(g), "/api/things")
	if full != "/api/things" {
		t.Errorf("group module saw %q, want the full path", full)
	}
}

func TestModule_Prefix(t *testing.T) {
	if got := web.NewModule(web.NewGroup("/api/v1")).Prefix(); got != "/api/v1" {
		t.Errorf("Prefix() = %q, want /api/v1", got)
	}
	if got := web.NewHandlerModule("/app", ok()).Prefix(); got != "/app" {
		t.Errorf("Prefix() = %q, want /app", got)
	}
}
