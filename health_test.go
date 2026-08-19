package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/internal/webtest"
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
	rec := webtest.Probe(web.Readiness(), web.ReadyPath)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 with no checks registered", rec.Code)
	}
	if _, ok := decodeBody(t, rec)["checks"]; ok {
		t.Error("checks is present but no checks were registered")
	}
}

func TestReadiness_AllReady(t *testing.T) {
	handler := web.Readiness(
		web.Check{Name: "lifecycle", Checker: staticChecker(true)},
		web.Check{Name: "database", Checker: staticChecker(true)},
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
		web.Check{Name: "lifecycle", Checker: staticChecker(true)},
		web.Check{Name: "database", Checker: staticChecker(false)},
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

func TestReadiness_NilCheckerIsNotReady(t *testing.T) {
	rec := webtest.Probe(web.Readiness(web.Check{Name: "database"}), web.ReadyPath)
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
	web.RegisterHealth(mux, web.Check{Name: "lifecycle", Checker: lc})

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
	web.RegisterHealth(mux, web.Check{Name: "lifecycle", Checker: staticChecker(true)})

	for _, path := range []string{web.HealthPath, web.ReadyPath} {
		if got := webtest.Probe(mux, path).Code; got != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, got)
		}
	}
}

func TestRegisterHealth_RejectsOtherMethods(t *testing.T) {
	mux := http.NewServeMux()
	web.RegisterHealth(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, web.HealthPath, nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s = %d, want 405 from the method-prefixed pattern", web.HealthPath, rec.Code)
	}
}
