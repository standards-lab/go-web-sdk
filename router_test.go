package web_test

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// module compiles a one-route group answering GET <prefix><pattern>.
func module(prefix, pattern string) *web.Module {
	g := web.NewGroup(prefix)
	g.Handle(http.MethodGet, pattern, ok())
	return web.NewModule(g)
}

// Prefixes match on segment boundaries: /api/v10 shares the string prefix
// /api/v1 but is a different segment, so it falls through to the native mux.
func TestRouter_LongestPrefixMatchOnSegmentBoundaries(t *testing.T) {
	r := web.NewRouter()
	r.Mount(module("/api", "/status"))
	r.Mount(module("/api/v1", "/orders"))

	for path, want := range map[string]int{
		"/api/v1/orders": http.StatusOK,       // the longer mount wins
		"/api/status":    http.StatusOK,       // the shorter mount still serves its own
		"/api/v1":        http.StatusNotFound, // owned by the longer mount, no route binds it
		"/api/v10/x":     http.StatusNotFound, // string prefix, different segment
	} {
		if got := webtest.Probe(r, path).Code; got != want {
			t.Errorf("GET %s = %d, want %d", path, got, want)
		}
	}
}

func TestRouter_FallsBackToNativeMux(t *testing.T) {
	r := web.NewRouter()
	r.Mount(module("/api", "/things"))
	r.Handle("GET /status", ok())

	if got := webtest.Probe(r, "/status").Code; got != http.StatusOK {
		t.Errorf("GET /status = %d, want 200 from the native mux", got)
	}
	if got := webtest.Probe(r, "/nowhere").Code; got != http.StatusNotFound {
		t.Errorf("GET /nowhere = %d, want 404", got)
	}
}

// *Router satisfies Mounter, so RegisterHealth mounts the probes on the
// native mux — structurally outside every module's middleware, with no
// exemption logic.
func TestRouter_ProbesMountOutsideModuleMiddleware(t *testing.T) {
	var order []string

	g := web.NewGroup("/api")
	g.Use(tag(&order, "module"))
	g.Handle(http.MethodGet, "/things", ok())

	r := web.NewRouter()
	r.Mount(web.NewModule(g))
	web.RegisterHealth(r, lifecycle.New())

	if got := webtest.Probe(r, web.HealthPath).Code; got != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", web.HealthPath, got)
	}
	// The coordinator never ran, so 503 rather than 200; what this test
	// checks is that the path is mounted and reaches the probe, not the
	// coordinator's readiness state.
	if got := webtest.Probe(r, web.ReadyPath).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("GET %s = %d, want 503 before the coordinator runs", web.ReadyPath, got)
	}
	if len(order) != 0 {
		t.Errorf("module middleware saw a probe request: %v", order)
	}
}

// Router middleware wraps the whole dispatch — module paths and the native
// fallback alike — and runs before any group middleware.
func TestRouter_UseWrapsWholeDispatch(t *testing.T) {
	var order []string

	g := web.NewGroup("/api")
	g.Use(tag(&order, "group"))
	g.Handle(http.MethodGet, "/things", ok())

	r := web.NewRouter()
	r.Use(tag(&order, "router"))
	r.Mount(web.NewModule(g))
	r.Handle("GET /status", ok())

	webtest.Probe(r, "/api/things")
	want := []string{"router", "group"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order = %v, want %v", order, want)
	}

	order = nil
	webtest.Probe(r, "/status")
	if len(order) != 1 || order[0] != "router" {
		t.Errorf("order = %v, want the router middleware on the fallback path", order)
	}
}

func TestRouter_SecondModuleAtSamePrefixPanics(t *testing.T) {
	r := web.NewRouter()
	r.Mount(module("/api", "/things"))

	mustPanic(t, "Mount at a taken prefix", func() {
		r.Mount(module("/api", "/other"))
	})
}

func TestRouter_UnmatchedPathIs404(t *testing.T) {
	r := web.NewRouter()
	if got := webtest.Probe(r, "/anything").Code; got != http.StatusNotFound {
		t.Errorf("GET /anything = %d, want 404 from an empty router", got)
	}
}
