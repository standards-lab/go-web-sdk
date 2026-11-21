package web_test

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/internal/handlertest"
)

func TestNewModule_RegistersFullPatterns(t *testing.T) {
	child := web.NewGroup("/orders")
	child.Handle(http.MethodGet, "/recent", ok())

	root := web.NewGroup("/api/v1")
	root.Handle(http.MethodGet, "/status", ok())
	root.Mount(child)

	m := web.NewModule(root)
	for _, path := range []string{"/api/v1/status", "/api/v1/orders/recent"} {
		if got := handlertest.Get(m, path).Code; got != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, got)
		}
	}
	if got := handlertest.Get(m, "/orders/recent").Code; got != http.StatusNotFound {
		t.Errorf("GET /orders/recent = %d, want 404 without the full prefix", got)
	}
}

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

	handlertest.Get(web.NewModule(root), "/api/orders/recent")

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

	handlertest.Get(m, "/api/things")
	handlertest.Get(m, "/api/things")

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

func TestNewHandlerModule_StripsThePrefix(t *testing.T) {
	var stripped, full string

	hm := web.NewHandlerModule("/app", http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			stripped = r.URL.Path
		}))
	handlertest.Get(hm, "/app/index.html")
	if stripped != "/index.html" {
		t.Errorf("handler module saw %q, want the prefix stripped", stripped)
	}

	g := web.NewGroup("/api")
	g.Handle(http.MethodGet, "/things", http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			full = r.URL.Path
		}))
	handlertest.Get(web.NewModule(g), "/api/things")
	if full != "/api/things" {
		t.Errorf("group module saw %q, want the full path", full)
	}
}

func TestModule_MissIsAProblem(t *testing.T) {
	g := web.NewGroup("/api")
	g.Handle(http.MethodGet, "/things", ok())
	m := web.NewModule(g)

	expectProblem(t, handlertest.Serve(m, http.MethodGet, "/api/nowhere"), http.StatusNotFound, "Not Found", "/api/nowhere")

	rec := handlertest.Serve(m, http.MethodDelete, "/api/things")
	expectProblem(t, rec, http.StatusMethodNotAllowed, "Method Not Allowed", "/api/things")
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q, want the mux's computed set preserved", allow)
	}
}

func TestModule_RedirectsStillRedirect(t *testing.T) {
	m := module("/api", "/things")

	rec := handlertest.Get(m, "/api//nowhere")
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/api/nowhere" {
		t.Errorf("GET /api//nowhere = %d, Location %q; want 307 to /api/nowhere", rec.Code, rec.Header().Get("Location"))
	}
}

func TestModule_GroupSettersOverrideTheDefaults(t *testing.T) {
	g := web.NewGroup("/api")
	g.Handle(http.MethodGet, "/things", ok())
	g.SetNotFound(custom(http.StatusNotFound))
	g.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Allow-Seen", w.Header().Get("Allow"))
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	m := web.NewModule(g)

	rec := handlertest.Serve(m, http.MethodGet, "/api/nowhere")
	if rec.Code != http.StatusNotFound || rec.Header().Get("X-Custom") != "yes" {
		t.Errorf("GET /api/nowhere = %d, X-Custom %q; want the custom 404", rec.Code, rec.Header().Get("X-Custom"))
	}
	rec = handlertest.Serve(m, http.MethodPost, "/api/things")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("X-Allow-Seen") != "GET, HEAD" {
		t.Errorf("POST /api/things = %d, X-Allow-Seen %q; want the custom 405 with Allow visible", rec.Code, rec.Header().Get("X-Allow-Seen"))
	}
}

func TestModule_MissRunsNoGroupMiddleware(t *testing.T) {
	var order []string

	child := web.NewGroup("/orders")
	child.Use(tag(&order, "leaf"))
	child.Handle(http.MethodGet, "/recent", ok())
	root := web.NewGroup("/api")
	root.Use(tag(&order, "root"))
	root.Mount(child)

	r := web.NewRouter()
	r.Use(tag(&order, "router"))
	r.Mount(web.NewModule(root))

	handlertest.Get(r, "/api/orders/nowhere")
	if len(order) != 1 || order[0] != "router" {
		t.Errorf("order = %v, want only the router middleware on a module miss", order)
	}
	order = nil
	handlertest.Serve(r, http.MethodPost, "/api/orders/recent")
	if len(order) != 1 || order[0] != "router" {
		t.Errorf("order = %v, want only the router middleware on a module 405", order)
	}
}

func TestNewModule_MissHandlerOnNestedGroupPanics(t *testing.T) {
	for name, set := range map[string]func(*web.Group){
		"SetNotFound":         func(g *web.Group) { g.SetNotFound(ok()) },
		"SetMethodNotAllowed": func(g *web.Group) { g.SetMethodNotAllowed(ok()) },
	} {
		child := web.NewGroup("/orders")
		set(child)
		root := web.NewGroup("/api")
		root.Mount(child)
		mustPanic(t, name+" on a nested group at NewModule", func() {
			web.NewModule(root)
		})
	}
}

func TestGroup_MissSettersAfterNewModulePanic(t *testing.T) {
	g := web.NewGroup("/api")
	web.NewModule(g)

	mustPanic(t, "SetNotFound after NewModule", func() {
		g.SetNotFound(ok())
	})
	mustPanic(t, "SetMethodNotAllowed after NewModule", func() {
		g.SetMethodNotAllowed(ok())
	})
}
