# go-web-sdk

The Application SDK for web services of Go Elemental, the Standards Lab organization's Go
implementation of the Elemental Architecture: the HTTP server and its configuration, routing, RFC 9457
problem responses, the liveness and readiness probes, and middleware.

The design and conventions of this repository are documented in the organization's
[documentation landing zone](https://github.com/standards-lab/docs); this context records only
working knowledge the landing zone and the code do not express. The repository page is
[go-web-sdk](https://github.com/standards-lab/docs/blob/main/standards/go-elemental/go-web-sdk/index.md),
under the [Go Elemental](https://github.com/standards-lab/docs/blob/main/standards/go-elemental/index.md)
standard, with the design detailed in its server, routing, problem responses, health, and
middleware pages.

## Capability map

The code and each package's `doc.go` are authoritative for what is built; the landing zone
documents the design. An unbuilt capability gains written detail when a session is about to
build it.

- **web** — the HTTP layer: the bind-then-serve server declared as go-core's root-stage
  lifecycle service, route groups, modules, and the router, the probes aggregating
  `lifecycle.Check` values, the problem writers, the `Middleware` type with `Chain`, the read
  contract (`ParseQuery` splitting a request's query string into paging and an ordered filter
  list with the bracket operator grammar, as one `Query` under caller-supplied `Limits`; the
  `Page[T]` envelope), the request helpers (`IfMatch`, `DecodeJSON`), the `ErrorWriter` mapping
  returned errors to problem responses — its own error types map themselves, sealed, and
  consumer-supplied matchers decide the rest, with `Detail` and `Log` set at wiring — and the
  error-returning handler adapter (`HandlerFunc`, `Handle`, `Group.HandleErr` under
  `Group.SetErrorWriter`) that never writes a second response. Built, v0.6.0.
- **middleware** — the middleware implementations: the request logger. Built.
- **Candidate direction** — what remains of the adapter story after v0.6.0: the recorder
  exported and the logger rewritten onto it, the problem vocabulary, router 404/405 hooks, the
  `ErrorLog` bridge (`concepts/error-handling.md`); the middleware set
  (`concepts/middleware-sourcing.md`); a readiness type hook (`concepts/direction.md`). The
  roadmap's `v1.web` goal sequences them. Where service-collaborating middleware lives is
  settled in `concepts/service-middleware.md`.
