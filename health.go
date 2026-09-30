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
		w.Header().Set("Cache-Control", "no-store")
		_ = WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

// Readiness answers 200 with each check's state when every check is ready,
// and otherwise notReady as a 503 problem whose "checks" member carries the
// states; a Check with a nil Checker is not ready. A zero member of notReady
// takes its default (Type about:blank, Title the status phrase, Detail "one
// or more readiness checks failed"), and its Status and any "checks" in its
// Extras are replaced.
func Readiness(notReady Problem, checks ...lifecycle.Check) http.Handler {
	return readiness(notReady, func() []lifecycle.Check { return checks })
}

// readiness is [Readiness] over checks read on every request.
func readiness(notReady Problem, checks func() []lifecycle.Check) http.Handler {
	notReady.Status = http.StatusServiceUnavailable
	if notReady.Detail == "" {
		notReady.Detail = "one or more readiness checks failed"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		participants := checks()
		results := make([]checkResult, 0, len(participants))
		ready := true
		for _, check := range participants {
			ok := check.Checker != nil && check.Checker.Ready()
			ready = ready && ok
			results = append(results, checkResult{Name: check.Name, Ready: ok})
		}

		w.Header().Set("Cache-Control", "no-store")
		if !ready {
			p := notReady
			p.Extras = make(map[string]any, len(notReady.Extras)+1)
			maps.Copy(p.Extras, notReady.Extras)
			p.Extras["checks"] = results
			_ = p.WriteFor(w, r)
			return
		}
		_ = WriteJSON(w, http.StatusOK, readyBody{Status: "ready", Checks: results})
	})
}

// RegisterHealth mounts [Liveness] at GET /healthz and [Readiness] at GET
// /readyz over lc: the coordinator itself, as "lifecycle", then its Checks,
// read on every request, so a service added after this call still appears.
func RegisterHealth(m Mounter, lc *lifecycle.Coordinator, notReady Problem) {
	m.Handle("GET "+HealthPath, Liveness())
	m.Handle("GET "+ReadyPath, readiness(notReady, func() []lifecycle.Check {
		return append([]lifecycle.Check{{Name: "lifecycle", Checker: lc}}, lc.Checks()...)
	}))
}
