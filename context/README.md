# go-web-sdk

The application SDK for web services of `go-minimal`, the Standards Lab organization's
minimal-dependency Go standard. This is the SDK that makes a web service:

- the HTTP server and its configuration,
- routing,
- RFC 9457 problem responses,
- the liveness and readiness probes, and
- middleware.

## What we're building toward

- The lowest practical level of abstraction, no frameworks: the stdlib `net/http` transport, with
  the SDK supplying what every web service otherwise hand-writes around it. Dependencies flow
  downward only; this repository depends on `go-core` and is unaware of what runs above it.
- A standard tier and nothing else. The SDK's standard tier is exactly HTTP's common standard —
  RFC 9110 for the protocol, RFC 9457 for problem responses — and it has no providers: nothing
  changes on a provider swap because there is nothing to swap.
- One Go module, versioned and released as one artifact on root tags.

## Repository topology

The repository is a single Go module rooted at `github.com/standards-lab/go-web-sdk`. The `web`
package occupies the module root, and `middleware` is its one sub-package. The module depends on
the standard library and `go-core`; there are no sub-modules. See `design/topology-and-naming.md`.

## Capability map

The code and each package's `doc.go` are authoritative for what is built; an unbuilt capability
gains written detail when a session is about to build it.

- **web** — the HTTP layer: the bind-then-serve `Server` wired for go-core's lifecycle and its
  tri-state configuration block; route groups, modules, and the router, compiled once at wiring
  time; the `/healthz` and `/readyz` probes; RFC 9457 problem writers and the JSON writer; and the
  `Middleware` type with `Chain`. Built.
- **middleware** — the middleware implementations: the request logger. Built.
- **Candidate direction** — the rest of the middleware set, error mapping, the success envelope and
  the page response, and a readiness type hook (`concepts/direction.md`); each waits on a consumer,
  and the roadmap re-plan decides what is next.

## How this repository works

- **Topology and naming** — one module, the cohesive `web` package, the `middleware` instances
  package, the tag convention. See `design/topology-and-naming.md`.
- **Dependencies** — what the module may depend on, what it takes from `go-core`, and what it
  deliberately does not. See `design/dependencies.md`.
- **The server** — why the bootstrap is in the SDK, bind-then-serve, and the lifecycle wiring. See
  `design/server.md`.
- **Routing** — groups, modules, and the router; compose once at wiring time. See
  `design/routing.md`.
- **Health** — the probes report, they do not check; readiness is non-monotonic. See
  `design/health.md`.
- **Problem responses** — the SDK defines no problem types. See `design/problems.md`.
- **Middleware** — middleware belongs to the transport; the type in `web`, the implementations in
  `middleware`. See `design/middleware.md`.
- **Releases and CI** — root tags from `CHANGELOG.md`, the CI checks, the `mise` tasks. See
  `design/release-and-ci.md`.
- **Tests and documentation** — co-located, black-box, hermetic tests; shared helpers in
  `internal/webtest`; `doc.go` ownership. See `design/tests-and-docs.md`.
