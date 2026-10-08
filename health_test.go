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

// staticChecker reports a fixed readiness, standing in for a subsystem that
// satisfies lifecycle.ReadinessChecker.
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

// A probe's answer is the state of this moment: no cache may keep it, the
// 503 included, or a recovered service would read as down.
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
	// The failing participant has to be identifiable, which is the reason the
	// extension carries names at all.
	failing, ok := checks[1].(map[string]any)
	if !ok {
		t.Fatalf("checks[1] = %v, want an object", checks[1])
	}
	if failing["name"] != "database" || failing["ready"] != false {
		t.Errorf("checks[1] = %v, want database reporting not ready", failing)
	}
}

// A consumer's own problem reaches the wire: the type hook this step adds.
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

// The checks member is always the probe's own: a consumer's Extras cannot
// shadow it.
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

// The status is always the probe's own: a consumer's Status is ignored.
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

// notReady.Extras is read, never written: several requests probing the same
// handler concurrently must not race on it (run under -race), and each
// must see its own "checks" entry rather than one leaking into another's
// document.
func TestReadiness_ConcurrentRequestsDoNotShareExtras(t *testing.T) {
	handler := web.Readiness(
		web.Problem{Extras: map[string]any{"tenant": "acme"}},
		lifecycle.Check{Name: "database", Checker: staticChecker(false)},
	)

	// t.Fatalf (inside the decodeBody helper) must only run on the test's
	// own goroutine, so each worker decodes inline and reports with
	// Errorf, which is safe to call concurrently.
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

// TestReadiness_TracksCoordinator walks the readiness signal through a whole
// graph Run: warming up while a Starter blocks, ready once OnReady fires,
// and stopped once Run returns. The middle and last transitions are what make
// /readyz useful to an orchestrator; /healthz answers 200 throughout.
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

// TestReadiness_TracksReadinessNode walks the same Run as
// TestReadiness_TracksCoordinator, but with the probes mounted inside a node
// constructor over a lifecycle.Readiness node, the wiring a graph uses since
// the Coordinator does not exist while the graph builds. /readyz follows the
// Coordinator New bound the Readiness to: 503 while a Starter blocks, 200
// once OnReady fires, 503 after Run returns; /healthz answers 200 throughout.
func TestReadiness_TracksReadinessNode(t *testing.T) {
	g := graph.New()
	readiness := g.Define("readiness", func(*graph.Scope) (*lifecycle.Readiness, error) {
		return new(lifecycle.Readiness), nil
	})
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
	// The coordinator never ran, so it reports not ready; readyz still has to
	// be mounted and answer, just not with 200.
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

// TestRegisterHealth_QueriesCoordinatorLive pins that a ReadinessChecker
// node defined in the graph appears on /readyz once New runs, after the
// coordinator's own "lifecycle" check, and that its state is read on every
// request rather than snapshotted when RegisterHealth mounts the probe.
func TestRegisterHealth_QueriesCoordinatorLive(t *testing.T) {
	g := graph.New()
	database := g.Define("database", func(*graph.Scope) (*toggleChecker, error) {
		return &toggleChecker{}, nil
	})
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

// A nil Doctor is a wiring mistake: RegisterHealth panics naming the fix
// rather than mounting a probe that nil-dereferences on its first request.
func TestRegisterHealth_PanicsOnNilDoctor(t *testing.T) {
	defer func() {
		const want = "web: RegisterHealth requires a Doctor; pass the *lifecycle.Coordinator or a *lifecycle.Readiness node's value"
		if r := recover(); r != want {
			t.Fatalf("panic = %v, want %q", r, want)
		}
	}()
	web.RegisterHealth(http.NewServeMux(), nil, web.Problem{})
}

// TestRegisterHealth_ReadsAReadinessNodeLive mounts the probes inside a node
// constructor, over a lifecycle.Readiness the Coordinator does not exist yet
// to bind. Until New binds it, /readyz reports "lifecycle" alone, not ready;
// once New has, the same probe reports "lifecycle" then the System's
// ReadinessChecker nodes, layer 0 before layer 1 and, within a layer, in
// definition order rather than the order the constructors reached them.
// Nothing else registers a check.
func TestRegisterHealth_ReadsAReadinessNodeLive(t *testing.T) {
	g := graph.New()
	readiness := g.Define("readiness", func(*graph.Scope) (*lifecycle.Readiness, error) {
		return new(lifecycle.Readiness), nil
	})
	queue := g.Define("queue", func(*graph.Scope) (*toggleChecker, error) {
		return &toggleChecker{}, nil
	})
	database := g.Define("database", func(*graph.Scope) (*toggleChecker, error) {
		return &toggleChecker{}, nil
	})
	search := g.Define("search", func(s *graph.Scope) (*toggleChecker, error) {
		s.Use(database)
		return &toggleChecker{}, nil
	})
	var duringBuild *httptest.ResponseRecorder
	mux := g.Define("mux", func(s *graph.Scope) (*http.ServeMux, error) {
		m := http.NewServeMux()
		web.RegisterHealth(m, s.Use(readiness), web.Problem{})
		// search reaches database before queue is used, so discovery order
		// differs from definition order within layer 0.
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

// TestRegisterHealth_ServerNodeAddsNoCheck runs a System whose server node
// serves the probes over a Readiness node: the Coordinator starts the
// Server, so /readyz answers over the network, ready, with "lifecycle" as
// its only check, and shuts it down when Run ends.
func TestRegisterHealth_ServerNodeAddsNoCheck(t *testing.T) {
	g := graph.New()
	readiness := g.Define("readiness", func(*graph.Scope) (*lifecycle.Readiness, error) {
		return new(lifecycle.Readiness), nil
	})
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
	// The Coordinator shut the Server down: its listener is closed, so a
	// fresh connection is refused.
	if conn, err := net.Dial("tcp", sys.Get(server).Addr()); err == nil {
		_ = conn.Close()
		t.Errorf("dial %s after Run returned succeeded, want the Server shut down", sys.Get(server).Addr())
	}
}

// readyChecks returns the checks a /readyz response body carries, each as
// "name=ready", in the order the probe reported them.
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

// coordinatorConfig is a finalized lifecycle.Config, whose positive
// ShutdownTimeout lifecycle.New requires.
var coordinatorConfig = lifecycle.Config{ShutdownTimeout: config.Duration(2 * time.Second)}

// build builds g from roots, failing the test on a constructor error.
func build(t *testing.T, g *graph.Graph, roots ...graph.Ref) *graph.System {
	t.Helper()
	sys, err := g.Build(roots...)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return sys
}

// idleCoordinator returns a coordinator over an empty System that never
// runs, so it reports not ready.
func idleCoordinator(t *testing.T) *lifecycle.Coordinator {
	t.Helper()
	return lifecycle.New(build(t, graph.New()), coordinatorConfig)
}

// expectProbes fails the test unless /readyz answers readyz and /healthz
// answers 200: liveness is independent of readiness, since the process is
// serving either way.
func expectProbes(t *testing.T, h http.Handler, when string, readyz int) {
	t.Helper()
	if got := handlertest.Get(h, web.ReadyPath).Code; got != readyz {
		t.Errorf("readyz = %d %s, want %d", got, when, readyz)
	}
	if got := handlertest.Get(h, web.HealthPath).Code; got != http.StatusOK {
		t.Errorf("healthz = %d %s, want 200", got, when)
	}
}

// blockingStarter is a lifecycle.Starter whose Start closes started, then
// blocks until release is closed, holding the coordinator in startup.
type blockingStarter struct {
	started, release chan struct{}
}

func (s *blockingStarter) Start(context.Context) error {
	close(s.started)
	<-s.release
	return nil
}

// toggleChecker is a lifecycle.ReadinessChecker whose state a test flips.
type toggleChecker struct {
	ready atomic.Bool
}

func (c *toggleChecker) Ready() bool { return c.ready.Load() }
