package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/internal/handlertest"
)

func module(prefix, pattern string) *web.Module {
	g := web.NewGroup(prefix)
	g.Handle(http.MethodGet, pattern, ok())
	return web.NewModule(g)
}

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

func custom(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(status)
	})
}

func TestRouter_LongestPrefixMatchOnSegmentBoundaries(t *testing.T) {
	r := web.NewRouter()
	r.Mount(module("/api", "/status"))
	r.Mount(module("/api/v1", "/orders"))

	for _, tt := range []struct {
		name string
		path string
		want int
	}{
		{"the longer mount wins", "/api/v1/orders", http.StatusOK},
		{"the shorter mount still serves its own", "/api/status", http.StatusOK},
		{"the longer mount owns its bare prefix, which no route binds", "/api/v1", http.StatusNotFound},
		{"a string prefix in a different segment does not match", "/api/v10/x", http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := handlertest.Get(r, tt.path).Code; got != tt.want {
				t.Errorf("GET %s = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}

func TestRouter_FallsBackToNativeMux(t *testing.T) {
	r := web.NewRouter()
	r.Mount(module("/api", "/things"))
	r.Handle("GET /status", ok())

	if got := handlertest.Get(r, "/status").Code; got != http.StatusOK {
		t.Errorf("GET /status = %d, want 200 from the native mux", got)
	}
	if got := handlertest.Get(r, "/nowhere").Code; got != http.StatusNotFound {
		t.Errorf("GET /nowhere = %d, want 404", got)
	}
}

func TestRouter_ProbesMountOutsideModuleMiddleware(t *testing.T) {
	var order []string

	g := web.NewGroup("/api")
	g.Use(tag(&order, "module"))
	g.Handle(http.MethodGet, "/things", ok())

	r := web.NewRouter()
	r.Mount(web.NewModule(g))
	web.RegisterHealth(r, idleCoordinator(t), web.Problem{})

	if got := handlertest.Get(r, web.HealthPath).Code; got != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", web.HealthPath, got)
	}
	if got := handlertest.Get(r, web.ReadyPath).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("GET %s = %d, want 503 before the coordinator runs", web.ReadyPath, got)
	}
	if len(order) != 0 {
		t.Errorf("module middleware saw a probe request: %v", order)
	}
}

func TestRouter_UseWrapsWholeDispatch(t *testing.T) {
	var order []string

	g := web.NewGroup("/api")
	g.Use(tag(&order, "group"))
	g.Handle(http.MethodGet, "/things", ok())

	r := web.NewRouter()
	r.Use(tag(&order, "router"))
	r.Mount(web.NewModule(g))
	r.Handle("GET /status", ok())

	handlertest.Get(r, "/api/things")
	want := []string{"router", "group"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order = %v, want %v", order, want)
	}

	order = nil
	handlertest.Get(r, "/status")
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
	if got := handlertest.Get(r, "/anything").Code; got != http.StatusNotFound {
		t.Errorf("GET /anything = %d, want 404 from an empty router", got)
	}
}

func TestRouter_MissIsAProblem(t *testing.T) {
	r := web.NewRouter()
	r.Handle("GET /status", ok())

	expectProblem(t, handlertest.Serve(r, http.MethodGet, "/nowhere"), http.StatusNotFound, "Not Found", "/nowhere")
	expectProblem(t, handlertest.Serve(r, http.MethodPost, "/nowhere"), http.StatusNotFound, "Not Found", "/nowhere")

	rec := handlertest.Serve(r, http.MethodPost, "/status")
	expectProblem(t, rec, http.StatusMethodNotAllowed, "Method Not Allowed", "/status")
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q, want the mux's computed set preserved", allow)
	}
}

func TestRouter_RedirectsStillRedirect(t *testing.T) {
	r := web.NewRouter()
	r.Handle("GET /things/", ok())

	for _, tt := range []struct {
		name     string
		path     string
		location string
	}{
		{"cleaned, though nothing matches the cleaned path", "//nowhere", "/nowhere"},
		{"trailing slash added for a registered pattern", "/things", "/things/"},
		{"cleaned, though nothing is there either", "/things/../", "/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := handlertest.Get(r, tt.path)
			if rec.Code != http.StatusTemporaryRedirect {
				t.Errorf("GET %s = %d, want 307; body %q", tt.path, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Location"); got != tt.location {
				t.Errorf("GET %s Location = %q, want %q", tt.path, got, tt.location)
			}
		})
	}
}

func TestRouter_AsteriskRequestURIIs400(t *testing.T) {
	r := web.NewRouter()
	if got := handlertest.Serve(r, http.MethodOptions, "*").Code; got != http.StatusBadRequest {
		t.Errorf("OPTIONS * = %d, want 400", got)
	}
}

func TestRouter_SetNotFoundAndSetMethodNotAllowedOverrideTheDefaults(t *testing.T) {
	r := web.NewRouter()
	r.Handle("GET /status", ok())
	r.SetNotFound(custom(http.StatusNotFound))
	r.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Allow-Seen", w.Header().Get("Allow"))
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))

	rec := handlertest.Serve(r, http.MethodGet, "/nowhere")
	if rec.Code != http.StatusNotFound || rec.Header().Get("X-Custom") != "yes" {
		t.Errorf("GET /nowhere = %d, X-Custom %q; want the custom 404", rec.Code, rec.Header().Get("X-Custom"))
	}
	rec = handlertest.Serve(r, http.MethodPost, "/status")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("X-Allow-Seen") != "GET, HEAD" {
		t.Errorf("POST /status = %d, X-Allow-Seen %q; want the custom 405 with Allow visible", rec.Code, rec.Header().Get("X-Allow-Seen"))
	}

	r.SetNotFound(nil)
	expectProblem(t, handlertest.Serve(r, http.MethodGet, "/nowhere"), http.StatusNotFound, "Not Found", "/nowhere")
}

func TestRouter_ModuleMissUsesTheModuleHandlers(t *testing.T) {
	g := web.NewGroup("/api")
	g.Handle(http.MethodGet, "/things", ok())
	g.SetNotFound(custom(http.StatusNotFound))

	r := web.NewRouter()
	r.Mount(web.NewModule(g))

	if rec := handlertest.Get(r, "/api/nowhere"); rec.Header().Get("X-Custom") != "yes" {
		t.Errorf("GET /api/nowhere did not reach the module's not-found handler: %d", rec.Code)
	}
	if rec := handlertest.Get(r, "/nowhere"); rec.Header().Get("X-Custom") != "" {
		t.Errorf("GET /nowhere reached the module's not-found handler")
	}
}

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

	handlertest.Get(r, "/api/things/42")
	if pattern != "GET /api/things/{id}" || id != "42" {
		t.Errorf("module route: Pattern = %q, PathValue(id) = %q; want the matched pattern and 42", pattern, id)
	}

	pattern, id = "", ""
	handlertest.Get(r, "/native/7")
	if pattern != "GET /native/{id}" || id != "7" {
		t.Errorf("native route: Pattern = %q, PathValue(id) = %q; want the matched pattern and 7", pattern, id)
	}
}
