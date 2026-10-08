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

// Readiness answers 200 with each check's state when every check is ready.
// Otherwise it answers notReady as a 503 problem whose "checks" member
// carries the states. A Check with a nil Checker is not ready, and zero
// checks report ready. A zero member of notReady takes its default (Type
// about:blank, Title the status phrase, Detail "one or more readiness checks
// failed"), and its Status and any "checks" in its Extras are replaced.
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

// Doctor is what [RegisterHealth] probes: its own readiness, reported as
// "lifecycle", and the readiness checks it aggregates. A
// *lifecycle.Coordinator is one, and so is a *lifecycle.Readiness, the
// graph node value that lets a node constructed before the Coordinator
// exists, such as the one that mounts the probes, report it.
type Doctor interface {
	lifecycle.ReadinessChecker
	Checks() []lifecycle.Check
}

// Both of go-core's readiness sources are Doctors, so RegisterHealth takes
// either.
var (
	_ Doctor = (*lifecycle.Coordinator)(nil)
	_ Doctor = (*lifecycle.Readiness)(nil)
)

// RegisterHealth mounts [Liveness] at GET /healthz and [Readiness] at GET
// /readyz. The readiness probe checks d itself, as "lifecycle", then d's
// Checks: for a Coordinator, or a Readiness bound to one, the System's
// ReadinessChecker values in layer order. It reads d on every request, so a
// Readiness bound after this call reports its Coordinator from then on;
// until then it reports "lifecycle" alone, not ready. It panics on a nil d.
func RegisterHealth(m Mounter, d Doctor, notReady Problem) {
	if d == nil {
		panic("web: RegisterHealth requires a Doctor; pass the *lifecycle.Coordinator or a *lifecycle.Readiness node's value")
	}
	m.Handle("GET "+HealthPath, Liveness())
	m.Handle("GET "+ReadyPath, readiness(notReady, func() []lifecycle.Check {
		return append([]lifecycle.Check{{Name: "lifecycle", Checker: d}}, d.Checks()...)
	}))
}
