# Where capability-collaborating middleware lives

Middleware that collaborates with an external capability — an authentication enforcement point that
verifies tokens, a tracing middleware that opens spans — is defined outside this SDK, against the
SDK's middleware platform: the `Middleware` type, `Chain`, and the router, group, and route hooks
that accept them. That much is settled. The SDK's own `middleware` package implements only
transport-generic middleware, because an application SDK and a capability never import each other,
and this SDK has no sub-modules — a middleware that imports a capability cannot land here without
breaking the module's dependency line.

Open is which outside home such a middleware is defined in:

- **The capability's repository, over `net/http` alone.** A `func(http.Handler) http.Handler`
  built from stdlib types is structurally a `web.Middleware` without importing this SDK, so a
  capability can offer its enforcement point in its own module and a composition root can hang it
  on a router unchanged. The cost: the capability compiles `net/http` and owns HTTP vocabulary,
  which `design/middleware.md`'s transport rule exists to avoid.
- **The consuming service.** The capability offers its collaborator (a verifier, a tracer); the
  service wraps it as middleware at its composition root. The cost: every service hand-writes the
  same adapter, which is the defect an SDK exists to remove.

A middleware whose dependency nothing else should compile is the same test that creates provider
sub-modules elsewhere in the estate; if one arrives, the answer may be a third shape. The decision
waits for the first real case — the authentication enforcement point is the likely forcing
consumer.
