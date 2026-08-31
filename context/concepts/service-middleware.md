# Where service-collaborating middleware lives

Middleware that collaborates with an infrastructure service — an authentication enforcement
point that verifies tokens, a tracing middleware that opens spans — is defined outside this SDK,
against the SDK's middleware platform: the `Middleware` type, `Chain`, and the router, group,
and route hooks that accept them. That much is settled. The SDK's own `middleware` package
implements only transport-generic middleware, because an application SDK and an infrastructure
library never import each other, and this SDK has no sub-modules — a middleware that imports an
infrastructure library cannot land here without breaking the module's dependency line.

Settled at the 2026-08-31 retrospective, in favor of the first home below: the infrastructure
library's repository, over stdlib types. The transport-vocabulary cost is accepted because the
alternative — every service hand-writing the same adapter — is the defect the SDK exists to
remove; the org's dependency-sourcing rule (`standards-lab
context/design/dependency-sourcing.md`, "Placement") records the same conclusion. The
`v1.web.middleware` and `goals.v1.auth` sessions express it. The original options, kept for
the reasoning:

- **The infrastructure library's repository, over `net/http` alone.** A
  `func(http.Handler) http.Handler` built from stdlib types is structurally a `web.Middleware`
  without importing this SDK, so an infrastructure library can offer its enforcement point in
  its own module and a composition root can hang it on a router unchanged. The cost: the library
  compiles `net/http` and owns HTTP vocabulary, which the transport rule in the landing zone's
  [middleware page](https://github.com/standards-lab/docs/blob/main/standards/go-minimal/go-web-sdk/middleware.md)
  exists to avoid.
- **The consuming service.** The infrastructure library offers its collaborator (a verifier, a
  tracer); the application wraps it as middleware at its composition root. The cost: every
  application hand-writes the same adapter, which is the defect an SDK exists to remove.

A middleware whose dependency nothing else should compile is the same test that creates provider
sub-modules elsewhere in the organization; if one arrives, the answer may be a third module
layout. The decision waits for the first real case — the authentication enforcement point is the
likely forcing consumer.
