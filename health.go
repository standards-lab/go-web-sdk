package web

import (
	"maps"
	"net/http"

	"github.com/standards-lab/go-core/lifecycle"
)

// The probe endpoints [RegisterHealth] mounts.
const (
	HealthPath = "/healthz"
	ReadyPath  = "/readyz"
)

// Mounter mounts a handler on a method-scoped pattern ("GET /healthz");
// *http.ServeMux satisfies it directly.
type Mounter interface {
	Handle(pattern string, handler http.Handler)
}

type checkResult struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

type readyBody struct {
	Status string        `json:"status"`
	Checks []checkResult `json:"checks,omitempty"`
}

// Liveness reports that the process is up and serving HTTP, and checks
// nothing else.
func Liveness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

// Readiness aggregates the checks: 200 with each participant's state when
// all are ready, and otherwise notReady written as the problem, at 503,
// carrying the participants as its "checks" extension member. A zero
// Problem takes the package defaults (about:blank, "one or more readiness
// checks failed"); any member notReady names — Type, Title, Detail, or
// Extras — is used instead. The status and the checks member are always
// the probe's own: notReady.Status is ignored, and a "checks" key in
// notReady.Extras is overwritten. Zero checks report ready.
func Readiness(notReady Problem, checks ...lifecycle.Check) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		results := make([]checkResult, 0, len(checks))
		ready := true
		for _, check := range checks {
			ok := check.Checker != nil && check.Checker.Ready()
			if !ok {
				ready = false
			}
			results = append(
				results,
				checkResult{Name: check.Name, Ready: ok},
			)
		}

		if !ready {
			p := notReady
			p.Status = http.StatusServiceUnavailable
			if p.Detail == "" {
				p.Detail = "one or more readiness checks failed"
			}
			extras := make(map[string]any, len(notReady.Extras)+1)
			maps.Copy(extras, notReady.Extras)
			extras["checks"] = results
			p.Extras = extras
			_ = p.WriteFor(w, r)
			return
		}

		_ = WriteJSON(w, http.StatusOK, readyBody{
			Status: "ready",
			Checks: results,
		})
	})
}

// RegisterHealth mounts [Liveness] at GET /healthz and a readiness probe at
// GET /readyz over lc, queried fresh on every request rather than once at
// registration: a service lc.Add adds after this call still appears on the
// next probe. notReady is [Readiness]'s own parameter, passed through
// unchanged. Exposing an in-progress service's check is safe only because
// the caller registers its own HTTP server at [lifecycle.StageRoot], so the
// coordinator never serves a request before every numbered stage exists.
func RegisterHealth(m Mounter, lc *lifecycle.Coordinator, notReady Problem) {
	readiness := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks := append([]lifecycle.Check{{Name: "lifecycle", Checker: lc}}, lc.Checks()...)
		Readiness(notReady, checks...).ServeHTTP(w, r)
	})

	m.Handle("GET "+HealthPath, Liveness())
	m.Handle("GET "+ReadyPath, readiness)
}
