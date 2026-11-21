package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/internal/handlertest"
)

type staticChecker bool

func (c staticChecker) Ready() bool { return bool(c) }

func TestLiveness_ReportsOK(t *testing.T) {
	rec := handlertest.Get(web.Liveness(), web.HealthPath)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != web.JSONMediaType {
		t.Errorf("Content-Type = %q, want %q", got, web.JSONMediaType)
	}
	if got := decodeBody(t, rec)["status"]; got != "ok" {
		t.Errorf("status = %v, want ok", got)
	}
}

func TestProbes_SendNoStore(t *testing.T) {
	for name, h := range map[string]http.Handler{
		"liveness":  web.Liveness(),
		"ready":     web.Readiness(web.Problem{}),
		"not ready": web.Readiness(web.Problem{}, lifecycle.Check{Name: "database", Checker: staticChecker(false)}),
	} {
		if got := handlertest.Get(h, web.ReadyPath).Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", name, got)
		}
	}
}

func TestReadiness_NoChecksIsReady(t *testing.T) {
	rec := handlertest.Get(web.Readiness(web.Problem{}), web.ReadyPath)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 with no checks registered", rec.Code)
	}
	if _, ok := decodeBody(t, rec)["checks"]; ok {
		t.Error("checks is present but no checks were registered")
	}
}

func TestReadiness_AllReady(t *testing.T) {
	handler := web.Readiness(
		web.Problem{},
		lifecycle.Check{Name: "lifecycle", Checker: staticChecker(true)},
		lifecycle.Check{Name: "database", Checker: staticChecker(true)},
	)

	rec := handlertest.Get(handler, web.ReadyPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != web.JSONMediaType {
		t.Errorf("Content-Type = %q, want %q", got, web.JSONMediaType)
	}

	body := decodeBody(t, rec)
	if got := body["status"]; got != "ready" {
		t.Errorf("status = %v, want ready", got)
	}
	if checks, ok := body["checks"].([]any); !ok || len(checks) != 2 {
		t.Errorf("checks = %v, want two entries", body["checks"])
	}
}

func TestReadiness_NotReadyEmitsProblem(t *testing.T) {
	handler := web.Readiness(
		web.Problem{},
		lifecycle.Check{Name: "lifecycle", Checker: staticChecker(true)},
		lifecycle.Check{Name: "database", Checker: staticChecker(false)},
	)

	rec := handlertest.Get(handler, web.ReadyPath)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != web.ProblemMediaType {
		t.Errorf("Content-Type = %q, want %q", got, web.ProblemMediaType)
	}

	body := decodeBody(t, rec)
	if got := body["type"]; got != web.ProblemTypeBlank {
		t.Errorf("type = %v, want %q", got, web.ProblemTypeBlank)
	}
	if got := body["title"]; got != "Service Unavailable" {
		t.Errorf("title = %v, want the status phrase", got)
	}
	if got := body["instance"]; got != web.ReadyPath {
		t.Errorf("instance = %v, want %s", got, web.ReadyPath)
	}

	checks, ok := body["checks"].([]any)
	if !ok || len(checks) != 2 {
		t.Fatalf("checks = %v, want two entries", body["checks"])
	}
	failing, ok := checks[1].(map[string]any)
	if !ok {
		t.Fatalf("checks[1] = %v, want an object", checks[1])
	}
	if failing["name"] != "database" || failing["ready"] != false {
		t.Errorf("checks[1] = %v, want database reporting not ready", failing)
	}
}

func TestReadiness_NotReadyConsumerProblemReachesTheWire(t *testing.T) {
	const problemType = "https://example.test/probs/not-ready"
	handler := web.Readiness(
		web.Problem{Type: problemType, Title: "Dependency unavailable", Detail: "database is down"},
		lifecycle.Check{Name: "database", Checker: staticChecker(false)},
	)

	rec := handlertest.Get(handler, web.ReadyPath)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	body := decodeBody(t, rec)
	if got := body["type"]; got != problemType {
		t.Errorf("type = %v, want %q", got, problemType)
	}
	if got := body["title"]; got != "Dependency unavailable" {
		t.Errorf("title = %v, want the consumer's title", got)
	}
	if got := body["detail"]; got != "database is down" {
		t.Errorf("detail = %v, want the consumer's detail", got)
	}
	if _, ok := body["checks"]; !ok {
		t.Error("checks is absent, want it alongside the consumer's problem")
	}
}

func TestReadiness_NotReadyChecksCannotBeOverriddenByExtras(t *testing.T) {
	handler := web.Readiness(
		web.Problem{Extras: map[string]any{"checks": "not the real checks"}},
		lifecycle.Check{Name: "database", Checker: staticChecker(false)},
	)

	rec := handlertest.Get(handler, web.ReadyPath)
	body := decodeBody(t, rec)
	checks, ok := body["checks"].([]any)
	if !ok || len(checks) != 1 {
		t.Fatalf("checks = %v, want the real one-entry check list", body["checks"])
	}
}

func TestReadiness_NotReadyStatusIsAlwaysServiceUnavailable(t *testing.T) {
	handler := web.Readiness(
		web.Problem{Status: http.StatusTeapot},
		lifecycle.Check{Name: "database", Checker: staticChecker(false)},
	)

	rec := handlertest.Get(handler, web.ReadyPath)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 despite the consumer's Status", rec.Code)
	}
}

func TestReadiness_ConcurrentRequestsDoNotShareExtras(t *testing.T) {
	handler := web.Readiness(
		web.Problem{Extras: map[string]any{"tenant": "acme"}},
		lifecycle.Check{Name: "database", Checker: staticChecker(false)},
	)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			rec := handlertest.Get(handler, web.ReadyPath)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", rec.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Errorf("decode body %q: %v", rec.Body.String(), err)
				return
			}
			if body["tenant"] != "acme" {
				t.Errorf("tenant = %v, want acme", body["tenant"])
			}
			checks, ok := body["checks"].([]any)
			if !ok || len(checks) != 1 {
				t.Errorf("checks = %v, want one entry", body["checks"])
			}
		}()
	}
	wg.Wait()
}

func TestReadiness_NilCheckerIsNotReady(t *testing.T) {
	rec := handlertest.Get(web.Readiness(web.Problem{}, lifecycle.Check{Name: "database"}), web.ReadyPath)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 for a nil checker", rec.Code)
	}
}

func TestReadiness_TracksCoordinator(t *testing.T) {
	g := graph.New()
	warmup := g.Define("warmup", func(*graph.Scope) (*blockingStarter, error) {
		return &blockingStarter{started: make(chan struct{}), release: make(chan struct{})}, nil
	})
	sys := build(t, g, warmup)
	lc := lifecycle.New(sys, coordinatorConfig)

	mux := http.NewServeMux()
	web.RegisterHealth(mux, lc, web.Problem{})

	ready := make(chan struct{})
	lc.OnReady(func() { close(ready) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- lc.Run(ctx) }()

	starter := sys.Get(warmup)
	recvOrFail(t, starter.started, "the Starter to start")
	expectProbes(t, mux, "during startup", http.StatusServiceUnavailable)

	close(starter.release)
	recvOrFail(t, ready, "coordinator to become ready")
	expectProbes(t, mux, "once ready", http.StatusOK)

	cancel()
	if err := recvOrFail(t, done, "Run to return"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	expectProbes(t, mux, "after Run returned", http.StatusServiceUnavailable)
}

func TestReadiness_TracksReadinessNode(t *testing.T) {
	g := graph.New()
	readiness := g.Fresh[lifecycle.Readiness]("readiness")
	warmup := g.Define("warmup", func(*graph.Scope) (*blockingStarter, error) {
		return &blockingStarter{started: make(chan struct{}), release: make(chan struct{})}, nil
	})
	muxNode := g.Define("mux", func(s *graph.Scope) (*http.ServeMux, error) {
		m := http.NewServeMux()
		web.RegisterHealth(m, s.Use(readiness), web.Problem{})
		return m, nil
	})
	sys := build(t, g, muxNode, warmup)
	lc := lifecycle.New(sys, coordinatorConfig)
	mux := sys.Get(muxNode)

	ready := make(chan struct{})
	lc.OnReady(func() { close(ready) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- lc.Run(ctx) }()

	starter := sys.Get(warmup)
	recvOrFail(t, starter.started, "the Starter to start")
	expectProbes(t, mux, "during startup", http.StatusServiceUnavailable)

	close(starter.release)
	recvOrFail(t, ready, "coordinator to become ready")
	expectProbes(t, mux, "once ready", http.StatusOK)

	cancel()
	if err := recvOrFail(t, done, "Run to return"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	expectProbes(t, mux, "after Run returned", http.StatusServiceUnavailable)
}

func TestRegisterHealth_MountsBothPaths(t *testing.T) {
	mux := http.NewServeMux()
	web.RegisterHealth(mux, idleCoordinator(t), web.Problem{})

	if got := handlertest.Get(mux, web.HealthPath).Code; got != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", web.HealthPath, got)
	}
	if got := handlertest.Get(mux, web.ReadyPath).Code; got != http.StatusServiceUnavailable {
		t.Errorf("GET %s = %d, want 503 before the coordinator runs", web.ReadyPath, got)
	}
}

func TestRegisterHealth_RejectsOtherMethods(t *testing.T) {
	mux := http.NewServeMux()
	web.RegisterHealth(mux, idleCoordinator(t), web.Problem{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, web.HealthPath, nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s = %d, want 405 from the method-prefixed pattern", web.HealthPath, rec.Code)
	}
}

func TestRegisterHealth_QueriesCoordinatorLive(t *testing.T) {
	g := graph.New()
	database := g.Fresh[toggleStarter]("database")
	sys := build(t, g, database)
	lc := lifecycle.New(sys, coordinatorConfig)
	mux := http.NewServeMux()
	web.RegisterHealth(mux, lc, web.Problem{})

	if got, want := readyChecks(t, handlertest.Get(mux, web.ReadyPath).Body.Bytes()), []string{"lifecycle=false", "database=false"}; !slices.Equal(got, want) {
		t.Errorf("checks = %v, want %v", got, want)
	}
	sys.Get(database).ready.Store(true)
	if got, want := readyChecks(t, handlertest.Get(mux, web.ReadyPath).Body.Bytes()), []string{"lifecycle=false", "database=true"}; !slices.Equal(got, want) {
		t.Errorf("checks after database became ready = %v, want %v, read live", got, want)
	}
}

func TestRegisterHealth_PanicsOnNilDoctor(t *testing.T) {
	defer func() {
		const want = "web: RegisterHealth requires a Doctor; pass the *lifecycle.Coordinator or a *lifecycle.Readiness node's value"
		if r := recover(); r != want {
			t.Fatalf("panic = %v, want %q", r, want)
		}
	}()
	web.RegisterHealth(http.NewServeMux(), nil, web.Problem{})
}

func TestRegisterHealth_ReadsAReadinessNodeLive(t *testing.T) {
	g := graph.New()
	readiness := g.Fresh[lifecycle.Readiness]("readiness")
	queue := g.Fresh[toggleStarter]("queue")
	database := g.Fresh[toggleStarter]("database")
	search := g.Define("search", func(s *graph.Scope) (*toggleStarter, error) {
		s.Use(database)
		return &toggleStarter{}, nil
	})
	var duringBuild *httptest.ResponseRecorder
	mux := g.Define("mux", func(s *graph.Scope) (*http.ServeMux, error) {
		m := http.NewServeMux()
		web.RegisterHealth(m, s.Use(readiness), web.Problem{})
		s.Use(search)
		s.Use(queue)
		duringBuild = handlertest.Get(m, web.ReadyPath)
		return m, nil
	})
	sys := build(t, g, mux)

	unbound := []string{"lifecycle=false"}
	if got := readyChecks(t, duringBuild.Body.Bytes()); !slices.Equal(got, unbound) {
		t.Errorf("checks during Build = %v, want %v", got, unbound)
	}
	if got := readyChecks(t, handlertest.Get(sys.Get(mux), web.ReadyPath).Body.Bytes()); !slices.Equal(got, unbound) {
		t.Errorf("checks before New = %v, want %v", got, unbound)
	}

	lifecycle.New(sys, coordinatorConfig)
	sys.Get(database).ready.Store(true)
	want := []string{"lifecycle=false", "queue=false", "database=true", "search=false"}
	if got := readyChecks(t, handlertest.Get(sys.Get(mux), web.ReadyPath).Body.Bytes()); !slices.Equal(got, want) {
		t.Errorf("checks once New bound the Readiness = %v, want %v", got, want)
	}
}

func TestRegisterHealth_ListsOnlyStarterChecks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mount func(*graph.System, *lifecycle.Coordinator, *graph.Node[*http.ServeMux]) http.Handler
	}{
		{"coordinator", func(_ *graph.System, lc *lifecycle.Coordinator, _ *graph.Node[*http.ServeMux]) http.Handler {
			mux := http.NewServeMux()
			web.RegisterHealth(mux, lc, web.Problem{})
			return mux
		}},
		{"readiness", func(sys *graph.System, _ *lifecycle.Coordinator, muxNode *graph.Node[*http.ServeMux]) http.Handler {
			return sys.Get(muxNode)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := graph.New()
			readiness := g.Fresh[lifecycle.Readiness]("readiness")
			database := g.Fresh[toggleStarter]("database")
			view := g.Fresh[toggleChecker]("view")
			muxNode := g.Define("mux", func(s *graph.Scope) (*http.ServeMux, error) {
				m := http.NewServeMux()
				web.RegisterHealth(m, s.Use(readiness), web.Problem{})
				return m, nil
			})
			sys := build(t, g, muxNode, database, view)
			lc := lifecycle.New(sys, coordinatorConfig)
			h := tc.mount(sys, lc, muxNode)

			ready := make(chan struct{})
			lc.OnReady(func() { close(ready) })
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- lc.Run(ctx) }()
			defer func() {
				cancel()
				if err := recvOrFail(t, done, "Run to return"); err != nil {
					t.Errorf("Run: %v", err)
				}
			}()
			recvOrFail(t, ready, "coordinator to become ready")

			sys.Get(database).ready.Store(true)
			sys.Get(view).ready.Store(true)
			rec := handlertest.Get(h, web.ReadyPath)
			if rec.Code != http.StatusOK {
				t.Errorf("readyz = %d with every check ready, want 200", rec.Code)
			}
			if got, want := readyChecks(t, rec.Body.Bytes()), []string{"lifecycle=true", "database=true"}; !slices.Equal(got, want) {
				t.Errorf("checks in the 200 = %v, want %v: no Check for a value with Ready and no Start", got, want)
			}

			sys.Get(database).ready.Store(false)
			sys.Get(view).ready.Store(false)
			rec = handlertest.Get(h, web.ReadyPath)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("readyz = %d with the Starter not ready, want 503", rec.Code)
			}
			if got, want := readyChecks(t, rec.Body.Bytes()), []string{"lifecycle=true", "database=false"}; !slices.Equal(got, want) {
				t.Errorf("checks in the 503 = %v, want %v: no Check for a value with Ready and no Start", got, want)
			}
		})
	}
}

func TestRegisterHealth_ServerNodeAddsNoCheck(t *testing.T) {
	g := graph.New()
	readiness := g.Fresh[lifecycle.Readiness]("readiness")
	cfg := finalized(t, web.Config{Host: "127.0.0.1", Port: new(0)})
	server := g.Define("server", func(s *graph.Scope) (*web.Server, error) {
		mux := http.NewServeMux()
		web.RegisterHealth(mux, s.Use(readiness), web.Problem{})
		return web.NewServer(cfg, mux, discard), nil
	})
	sys := build(t, g, server)
	lc := lifecycle.New(sys, coordinatorConfig)
	ready := make(chan struct{})
	lc.OnReady(func() { close(ready) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- lc.Run(ctx) }()
	recvOrFail(t, ready, "coordinator to become ready")

	res, err := http.Get("http://" + sys.Get(server).Addr() + web.ReadyPath)
	if err != nil {
		t.Fatalf("GET %s: %v", web.ReadyPath, err)
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatalf("read %s body: %v", web.ReadyPath, err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("readyz = %d, want 200", res.StatusCode)
	}
	if got, want := readyChecks(t, body), []string{"lifecycle=true"}; !slices.Equal(got, want) {
		t.Errorf("checks = %v, want %v: the Server is not a readiness check", got, want)
	}

	cancel()
	if err := recvOrFail(t, done, "Run to return"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if conn, err := net.Dial("tcp", sys.Get(server).Addr()); err == nil {
		_ = conn.Close()
		t.Errorf("dial %s after Run returned succeeded, want the Server shut down", sys.Get(server).Addr())
	}
}

func readyChecks(t *testing.T, body []byte) []string {
	t.Helper()
	var decoded struct {
		Checks []struct {
			Name  string `json:"name"`
			Ready bool   `json:"ready"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	out := make([]string, 0, len(decoded.Checks))
	for _, c := range decoded.Checks {
		out = append(out, fmt.Sprintf("%s=%t", c.Name, c.Ready))
	}
	return out
}

var coordinatorConfig = lifecycle.Config{ShutdownTimeout: config.Duration(2 * time.Second)}

func build(t *testing.T, g *graph.Graph, roots ...graph.Ref) *graph.System {
	t.Helper()
	sys, err := g.Build(roots...)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return sys
}

func idleCoordinator(t *testing.T) *lifecycle.Coordinator {
	t.Helper()
	return lifecycle.New(build(t, graph.New()), coordinatorConfig)
}

func expectProbes(t *testing.T, h http.Handler, when string, readyz int) {
	t.Helper()
	if got := handlertest.Get(h, web.ReadyPath).Code; got != readyz {
		t.Errorf("readyz = %d %s, want %d", got, when, readyz)
	}
	if got := handlertest.Get(h, web.HealthPath).Code; got != http.StatusOK {
		t.Errorf("healthz = %d %s, want 200", got, when)
	}
}

type blockingStarter struct {
	started, release chan struct{}
}

func (s *blockingStarter) Start(context.Context) error {
	close(s.started)
	<-s.release
	return nil
}

type toggleChecker struct {
	ready atomic.Bool
}

func (c *toggleChecker) Ready() bool { return c.ready.Load() }

type toggleStarter struct {
	toggleChecker
}

func (*toggleStarter) Start(context.Context) error { return nil }
