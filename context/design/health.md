# Health

The design of the liveness and readiness probes. The code and `doc.go` are authoritative for the
API; this note records the reasoning.

## The probes report; they do not check

`/healthz` reports that the process is up and serving HTTP and checks nothing else — that is what
makes an unanswered probe the liveness signal rather than a 500. `/readyz` aggregates whatever
`lifecycle.ReadinessChecker` participants the composition root supplies; the SDK contributes none of
its own. Each participant is named, so an operator can read which subsystem is failing the probe,
and a `Check` with a nil checker reports not ready — a subsystem that failed to construct fails the
probe rather than vanishing from it.

## Readiness is non-monotonic

The readiness signal follows go-core's lifecycle coordinator: ready once every startup hook
succeeds, not ready again the moment draining begins. A draining process reports 503 and stops
receiving traffic before its shutdown hooks run. Composition roots register the coordinator as the
first participant; each subsystem that reports its own readiness joins the list as it is
constructed.
