package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// probe serves one request of any method through h and returns the recorder.
func probe(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// expectProblem fails the test unless rec holds a problem document with the
// given status and title, about:blank type, and the path as instance.
func expectProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, title, path string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}
	if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
		t.Errorf("Content-Type = %q, want %q", ct, web.ProblemMediaType)
	}
	var p web.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not a problem document: %v: %s", err, rec.Body.String())
	}
	if p.Status != status || p.Title != title || p.Type != web.ProblemTypeBlank || p.Instance != path {
		t.Errorf("problem = %+v, want status %d, title %q, type about:blank, instance %q", p, status, title, path)
	}
	if p.Detail != "" {
		t.Errorf("detail = %q, want none", p.Detail)
	}
}

// custom is a handler that marks its response with a header, so a test can
// tell it apart from the default.
func custom(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(status)
	})
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
	web.RegisterHealth(r, lifecycle.New(), web.Problem{})

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

// A miss on the native mux is a problem document, not ServeMux's plain text.
func TestRouter_MissIsAProblem(t *testing.T) {
	r := web.NewRouter()
	r.Handle("GET /status", ok())

	expectProblem(t, probe(r, http.MethodGet, "/nowhere"), http.StatusNotFound, "Not Found", "/nowhere")
	expectProblem(t, probe(r, http.MethodPost, "/nowhere"), http.StatusNotFound, "Not Found", "/nowhere")

	rec := probe(r, http.MethodPost, "/status")
	expectProblem(t, rec, http.StatusMethodNotAllowed, "Method Not Allowed", "/status")
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q, want the mux's computed set preserved", allow)
	}
}

// ServeMux's own redirects — path cleaning and the trailing-slash
// redirect — are served as before, not swallowed into a 404.
func TestRouter_RedirectsStillRedirect(t *testing.T) {
	r := web.NewRouter()
	r.Handle("GET /things/", ok())

	for path, location := range map[string]string{
		"//nowhere":   "/nowhere", // cleaned; nothing matches the cleaned path
		"/things":     "/things/", // trailing-slash redirect to a registered pattern
		"/things/../": "/",        // cleaned; still nothing there
	} {
		rec := webtest.Probe(r, path)
		if rec.Code != http.StatusTemporaryRedirect {
			t.Errorf("GET %s = %d, want 307; body %q", path, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Location"); got != location {
			t.Errorf("GET %s Location = %q, want %q", path, got, location)
		}
	}
}

// The mux's guard against a "*" request-URI survives the change of dispatch.
func TestRouter_AsteriskRequestURIIs400(t *testing.T) {
	r := web.NewRouter()
	if got := probe(r, http.MethodOptions, "*").Code; got != http.StatusBadRequest {
		t.Errorf("OPTIONS * = %d, want 400", got)
	}
}

func TestRouter_SetNotFoundAndSetMethodNotAllowedOverrideTheDefaults(t *testing.T) {
	r := web.NewRouter()
	r.Handle("GET /status", ok())
	r.SetNotFound(custom(http.StatusNotFound))
	r.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The Allow header is on the response before the handler runs.
		w.Header().Set("X-Allow-Seen", w.Header().Get("Allow"))
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))

	rec := probe(r, http.MethodGet, "/nowhere")
	if rec.Code != http.StatusNotFound || rec.Header().Get("X-Custom") != "yes" {
		t.Errorf("GET /nowhere = %d, X-Custom %q; want the custom 404", rec.Code, rec.Header().Get("X-Custom"))
	}
	rec = probe(r, http.MethodPost, "/status")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("X-Allow-Seen") != "GET, HEAD" {
		t.Errorf("POST /status = %d, X-Allow-Seen %q; want the custom 405 with Allow visible", rec.Code, rec.Header().Get("X-Allow-Seen"))
	}

	// nil restores the default.
	r.SetNotFound(nil)
	expectProblem(t, probe(r, http.MethodGet, "/nowhere"), http.StatusNotFound, "Not Found", "/nowhere")
}

// A miss inside a module is the module's, answered by its group's handlers,
// not the router's.
func TestRouter_ModuleMissUsesTheModuleHandlers(t *testing.T) {
	g := web.NewGroup("/api")
	g.Handle(http.MethodGet, "/things", ok())
	g.SetNotFound(custom(http.StatusNotFound))

	r := web.NewRouter()
	r.Mount(web.NewModule(g))

	if rec := webtest.Probe(r, "/api/nowhere"); rec.Header().Get("X-Custom") != "yes" {
		t.Errorf("GET /api/nowhere did not reach the module's not-found handler: %d", rec.Code)
	}
	if rec := webtest.Probe(r, "/nowhere"); rec.Header().Get("X-Custom") != "" {
		t.Errorf("GET /nowhere reached the module's not-found handler")
	}
}

// A matched request reaches its handler with the fields ServeMux.ServeHTTP
// sets: the matched pattern and the path wildcards. ServeMux.Handler, which
// the miss discrimination uses, leaves both empty, so a hit must still be
// dispatched through ServeHTTP. Module routes and native-mux routes alike.
func TestRouter_MatchSetsPatternAndPathValues(t *testing.T) {
	var pattern, id string
	capture := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		pattern, id = r.Pattern, r.PathValue("id")
	})

	g := web.NewGroup("/api")
	g.Handle(http.MethodGet, "/things/{id}", capture)
	r := web.NewRouter()
	r.Mount(web.NewModule(g))
	r.Handle("GET /native/{id}", capture)

	webtest.Probe(r, "/api/things/42")
	if pattern != "GET /api/things/{id}" || id != "42" {
		t.Errorf("module route: Pattern = %q, PathValue(id) = %q; want the matched pattern and 42", pattern, id)
	}

	pattern, id = "", ""
	webtest.Probe(r, "/native/7")
	if pattern != "GET /native/{id}" || id != "7" {
		t.Errorf("native route: Pattern = %q, PathValue(id) = %q; want the matched pattern and 7", pattern, id)
	}
}
