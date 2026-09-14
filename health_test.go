package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// staticChecker reports a fixed readiness, standing in for a subsystem that
// satisfies lifecycle.ReadinessChecker.
type staticChecker bool

func (c staticChecker) Ready() bool { return bool(c) }

func TestLiveness_ReportsOK(t *testing.T) {
	rec := webtest.Probe(web.Liveness(), web.HealthPath)

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

func TestReadiness_NoChecksIsReady(t *testing.T) {
	rec := webtest.Probe(web.Readiness(web.Problem{}), web.ReadyPath)

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

	rec := webtest.Probe(handler, web.ReadyPath)
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

	rec := webtest.Probe(handler, web.ReadyPath)
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

	rec := webtest.Probe(handler, web.ReadyPath)
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

	rec := webtest.Probe(handler, web.ReadyPath)
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

	rec := webtest.Probe(handler, web.ReadyPath)
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
			rec := webtest.Probe(handler, web.ReadyPath)
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
	rec := webtest.Probe(web.Readiness(web.Problem{}, lifecycle.Check{Name: "database"}), web.ReadyPath)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 for a nil checker", rec.Code)
	}
}

// TestReadiness_TracksCoordinator walks the readiness signal through a whole
// process lifetime: warming up, ready, and stopped. The middle and last
// transitions are what make /readyz useful to an orchestrator.
func TestReadiness_TracksCoordinator(t *testing.T) {
	lc := lifecycle.New()

	mux := http.NewServeMux()
	web.RegisterHealth(mux, lc, web.Problem{})

	started := make(chan struct{})
	release := make(chan struct{})
	lc.OnStartup(func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	ready := make(chan struct{})
	lc.OnReady(func() { close(ready) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- lc.Run(ctx, 2*time.Second) }()

	recvOrFail(t, started, "startup hook to start")
	if got := webtest.Probe(mux, web.ReadyPath).Code; got != http.StatusServiceUnavailable {
		t.Errorf("status = %d during startup, want 503", got)
	}
	// Liveness is independent of readiness: the process is serving either way.
	if got := webtest.Probe(mux, web.HealthPath).Code; got != http.StatusOK {
		t.Errorf("healthz = %d during startup, want 200", got)
	}

	close(release)
	recvOrFail(t, ready, "coordinator to become ready")

	if got := webtest.Probe(mux, web.ReadyPath).Code; got != http.StatusOK {
		t.Errorf("status = %d after startup, want 200", got)
	}

	cancel()
	if err := recvOrFail(t, done, "Run to return"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := webtest.Probe(mux, web.ReadyPath).Code; got != http.StatusServiceUnavailable {
		t.Errorf("status = %d after Run returned, want 503", got)
	}
}

func TestRegisterHealth_MountsBothPaths(t *testing.T) {
	mux := http.NewServeMux()
	web.RegisterHealth(mux, lifecycle.New(), web.Problem{})

	if got := webtest.Probe(mux, web.HealthPath).Code; got != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", web.HealthPath, got)
	}
	// The coordinator never ran, so it reports not ready; readyz still has to
	// be mounted and answer, just not with 200.
	if got := webtest.Probe(mux, web.ReadyPath).Code; got != http.StatusServiceUnavailable {
		t.Errorf("GET %s = %d, want 503 before the coordinator runs", web.ReadyPath, got)
	}
}

func TestRegisterHealth_RejectsOtherMethods(t *testing.T) {
	mux := http.NewServeMux()
	web.RegisterHealth(mux, lifecycle.New(), web.Problem{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, web.HealthPath, nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s = %d, want 405 from the method-prefixed pattern", web.HealthPath, rec.Code)
	}
}

// TestRegisterHealth_QueriesCoordinatorLive is the regression test for the
// fix: RegisterHealth used to close over a snapshot of the coordinator's
// checks, so a service added afterward never appeared on the probe.
func TestRegisterHealth_QueriesCoordinatorLive(t *testing.T) {
	lc := lifecycle.New()
	mux := http.NewServeMux()
	web.RegisterHealth(mux, lc, web.Problem{})

	lc.Add(lifecycle.Service{
		Name:  "database",
		Stage: 0,
		Check: staticChecker(false),
	})

	rec := webtest.Probe(mux, web.ReadyPath)
	body := decodeBody(t, rec)
	checks, ok := body["checks"].([]any)
	if !ok || len(checks) != 2 {
		t.Fatalf("checks = %v, want the lifecycle check plus database, registered after RegisterHealth", body["checks"])
	}

	database, ok := checks[1].(map[string]any)
	if !ok || database["name"] != "database" {
		t.Fatalf("checks[1] = %v, want the late-registered database check", checks[1])
	}
	if database["ready"] != false {
		t.Errorf("database ready = %v, want false", database["ready"])
	}
}
