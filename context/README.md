# go-web-sdk

The Application SDK for web services of Go Minimal, the Standards Lab organization's
minimal-dependency Go standard: the HTTP server and its configuration, routing, RFC 9457
problem responses, the liveness and readiness probes, and middleware.

The design and conventions of this repository are documented in the organization's
[documentation landing zone](https://github.com/standards-lab/docs); this context records only
working knowledge the landing zone and the code do not express. The repository page is
[go-web-sdk](https://github.com/standards-lab/docs/blob/main/standards/go-minimal/go-web-sdk/index.md),
under the [Go Minimal](https://github.com/standards-lab/docs/blob/main/standards/go-minimal/index.md)
standard, with the design detailed in its server, routing, problem responses, health, and
middleware pages.

## Capability map

The code and each package's `doc.go` are authoritative for what is built; the landing zone
documents the design. An unbuilt capability gains written detail when a session is about to
build it.

- **web** — the HTTP layer: the bind-then-serve server declared as go-core's root-stage
  lifecycle service, route groups, modules, and the router, the probes aggregating
  `lifecycle.Check` values, the problem writers, the `Middleware` type with `Chain`, the read
  contract (`ParseQuery` splitting a request's query string into paging and filters as one
  `Query` under caller-supplied `Limits`, the `Page[T]` envelope), and the `ErrorWriter`
  mapping returned errors to problem responses through consumer-supplied matchers. Built.
- **middleware** — the middleware implementations: the request logger. Built.
- **Candidate direction** — the rest of the middleware set and a readiness type hook
  (`concepts/direction.md`); each waits on a consumer, and the roadmap re-plan
  decides what is next. Where service-collaborating middleware lives is open in
  `concepts/service-middleware.md`.
