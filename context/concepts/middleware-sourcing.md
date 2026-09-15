# The middleware set: hand-roll or source

Settled at the 2026-08-31 workspace retrospective. `v1.web.tasks.middleware`'s close (2026-09-14)
landed the hand-rolled catalog in full except path hygiene, and the org-wide rule and the
"standard library" markers live at the coordinator
(`standards-lab/context/design/dependency-sourcing.md`); this note now carries what remains: the
one deferred hand-rolled item, and the sourced set `goals.v1.middleware` has yet to adopt.

## The rule

The rule, its test for "trivial", the "standard but not Google" markers, and the obligations
on hand-rolled and sourced code are the coordinator's
`standards-lab/context/design/dependency-sourcing.md`; this note applies them middleware by
middleware.

## Path hygiene: deferred

Clean path, strip trailing slash. `net/http.ServeMux` already redirects an unclean path and
handles a trailing slash on its own, and the catalog's own condition — add only if a real
client needs behavior `ServeMux` doesn't already give it — has not fired. Recorded unbuilt at
`v1.web.tasks.middleware`'s close, not silently dropped.

## Sourced middleware

| Concern | Why not hand-roll | Standard library to take from |
|---|---|---|
| CORS | Preflight rules, `Vary` correctness, credentialed-origin restrictions, `Access-Control-Max-Age` semantics, Private Network Access headers. Wrong answers look right. | `github.com/rs/cors` |
| Real client IP | The parse is short; the threat model is not. `X-Forwarded-For` is attacker-controlled unless the trusted-proxy hop count is configured. | `chi/v5/middleware.RealIP` for the shape, but implement against a configured trusted-proxy list; see Adam Pritchard's "The perils of the 'real' client IP" for the model. |
| Compression | Correct `ResponseWriter` wrapping, content negotiation, skipping already-compressed media types, writer pooling, preserving `Flusher`. | `github.com/klauspost/compress/gzhttp` (the current standard; `NYTimes/gziphandler` is its ancestor) |
| Wrapped `ResponseWriter` | Preserving `Flusher`, `Hijacker`, `ReaderFrom`, `Pusher` through a wrap without an interface explosion. | None: the SDK's own `web.Recorder`, shared by `Handle`, `RequestLogger`, and `Recoverer` through `web.WrapWriter`. `Flusher`, `Hijacker`, and `ReaderFrom` reach through via `Unwrap` (`Flusher` also via `FlushError`, so a flush commits); `http.Pusher` does not. |
| Rate limiting | A single-process token bucket is easy; per-key with eviction, or distributed, is not. | `golang.org/x/time/rate` for the bucket (Go-team maintained); `github.com/go-chi/httprate` for the HTTP wrapper if per-key limiting is needed. |
| Token verification / JWKS | Cryptography. Never hand-roll. Lands in the auth infrastructure module, not the SDK (Placement, below). | `github.com/coreos/go-oidc/v3` for OIDC discovery + verification against Keycloak; `github.com/lestrrat-go/jwx/v2` if raw JWT/JWKS handling is required. |
| Tracing / metrics | Propagation formats and semantic conventions are a specification the industry converges on. | `go.opentelemetry.io/otel` and `otelhttp`. Infrastructure module, not the SDK. |

Every row above is imported as a declared dependency, never copied into the tree. `rs/cors`
passes the org's own top marker for a standard library (stdlib types at its boundary,
`func(http.Handler) http.Handler`), and chi's `RealIP` is the shape reference for the same
reason.

CORS, real client IP, compression, and rate limiting are consolidated into their own goal,
`v1.middleware`, split out of this task so their concrete adoption — versions, real-IP's
trusted-proxy configuration, and each library's own entry in this README's dependency-line
statement — gets its own planning session rather than this note prescribing it ahead of time.

## Placement

- Transport-generic, no infrastructure import → `go-web-sdk/middleware`.
- Collaborates with an infrastructure service (auth, tracing) → the infrastructure library's
  module, exposing a `func(http.Handler) http.Handler` over stdlib types so it is structurally
  a `web.Middleware` without importing the SDK. Settled at the retrospective, and the
  coordinator's placement obligation records the same conclusion.
- Copied third-party code → `go-web-sdk/middleware` with license header and upstream commit
  recorded in the file.

The reasoning behind the second placement, kept from the question it settled: an application
SDK and an infrastructure library never import each other, and this SDK has no sub-modules,
so a middleware that imports an infrastructure library cannot land here without breaking the
module's dependency line. The alternative home was the consuming service: the library would
offer only its collaborator (a verifier, a tracer), and every application would wrap it at its
composition root. That is the defect the SDK exists to remove, so the library owns HTTP
vocabulary instead, against the standard's rule that middleware belongs to the transport. A
middleware whose dependency nothing else should compile is the same test that creates provider
sub-modules elsewhere in the organization. If one arrives, the answer may be a third module
layout, and the authentication enforcement point is the likely forcing consumer.

## Dependency-line statement

The SDK's declared line is the README's: the standard library and go-core, with packages as
idiomatic and stable as the standard library admitted and vendor SDKs never. Sourcing under
this catalog is a stated enhancement per the coordinator's rule: the session that lands
the first sourced or copied middleware states the admitted categories (a specification
surface, a threat model; cryptography stays out of this SDK entirely) beside that principle.
A silent import no stated line covers is a defect.

## Maintenance obligation

The coordinator's obligation on hand-rolled code applies; for this SDK the release re-check is
against `net/http` changes (`http.ResponseController`, `ServeMux` pattern semantics, new
`http.Server` fields).
