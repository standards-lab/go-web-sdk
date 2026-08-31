# The middleware set: hand-roll or source

Settled at the 2026-08-31 workspace retrospective; `v1.web.middleware` in the coordinator's
roadmap cites this note. The org-wide rule and the "standard library" markers live at the
coordinator (`standards-lab/context/design/dependency-sourcing.md`); this note carries the
middleware-specific catalog — what the SDK hand-rolls, what it sources, and from where — plus
the retrospective's build findings. It decays into the landing-zone middleware page and the
code when the sessions land.

## The rule, applied to middleware

Implement in `go-web-sdk/middleware` any middleware that is transport-generic and has no
specification or security surface. Source from an industry-standard library — or copy its
implementation into the SDK with attribution — any middleware whose correctness depends on a
specification with known corner cases, a threat model, or cryptography. Never carry a
dependency for something the standard library already provides.

The test for "trivial" is not line count. It is whether the failure mode is visible in a unit
test you would think to write. A request-ID middleware fails loudly. A CORS middleware fails
by letting the wrong origin through with right-looking headers.

## Hand-roll in the SDK

Each is 10–50 lines over stdlib types, and the tests are obvious.

| Middleware | Notes |
|---|---|
| Request ID | Generate if absent, echo if present from a trusted hop, put on context, set response header. |
| Recoverer | `recover()`, log with the panic value, write a 500 problem if nothing was written. Shares the wrapped writer (`concepts/error-handling.md` §2.1). Replaces `net/http`'s built-in recovery only for the problem body. |
| Timeout | `context.WithTimeout` around the request; the handler observes `r.Context()`. Do not reimplement `http.TimeoutHandler`'s response buffering. |
| Request logger | Already built. Keep it in-house; it is wired to `slog` and the probe paths. |
| Content-type gate | Reject a command body whose `Content-Type` is not in an allowlist with a 415 problem. |
| Body limit | `http.MaxBytesReader` as middleware, so the limit is declared at the route rather than inside `decode`. |
| Fixed headers | Security headers, `Cache-Control: no-store` for API responses. A map and a loop. |
| Conditional wrap | `Maybe(mw, pred)` — apply a middleware only when a predicate on the request holds. |
| Path hygiene | Clean path, strip trailing slash. Prefer configuring `ServeMux` behavior first; add only if a real client needs it. |

## Source or copy with attribution

| Concern | Why not hand-roll | Standard library to take from |
|---|---|---|
| CORS | Preflight rules, `Vary` correctness, credentialed-origin restrictions, `Access-Control-Max-Age` semantics, Private Network Access headers. Wrong answers look right. | `github.com/rs/cors` |
| Real client IP | The parse is short; the threat model is not. `X-Forwarded-For` is attacker-controlled unless the trusted-proxy hop count is configured. | `chi/v5/middleware.RealIP` for the shape, but implement against a configured trusted-proxy list; see Adam Pritchard's "The perils of the 'real' client IP" for the model. |
| Compression | Correct `ResponseWriter` wrapping, content negotiation, skipping already-compressed media types, writer pooling, preserving `Flusher`. | `github.com/klauspost/compress/gzhttp` (the current standard; `NYTimes/gziphandler` is its ancestor) |
| Wrapped `ResponseWriter` | Preserving `Flusher`, `Hijacker`, `ReaderFrom`, `Pusher` through a wrap without an interface explosion. | `github.com/felixge/httpsnoop` — or the SDK's own recorder extended to cover all four; either way, one type shared by logger, adapter, recoverer. |
| Rate limiting | A single-process token bucket is easy; per-key with eviction, or distributed, is not. | `golang.org/x/time/rate` for the bucket (Go-team maintained); `github.com/go-chi/httprate` for the HTTP wrapper if per-key limiting is needed. |
| Token verification / JWKS | Cryptography. Never hand-roll. Lands in the auth infrastructure module, not the SDK, per `concepts/service-middleware.md` (settled). | `github.com/coreos/go-oidc/v3` for OIDC discovery + verification against Keycloak; `github.com/lestrrat-go/jwx/v2` if raw JWT/JWKS handling is required. |
| Tracing / metrics | Propagation formats and semantic conventions are a specification the industry converges on. | `go.opentelemetry.io/otel` and `otelhttp`. Infrastructure module, not the SDK. |

Copy-with-attribution is first-class for CORS and real-IP: both `rs/cors` and chi are
MIT-licensed, a few hundred lines, dependency-free. Prefer copying when the module line is the
only objection and the upstream change rate is low (`rs/cors` fits); prefer importing when
upstream moves with a spec (`otel`, `go-oidc`). Copied code carries the license header and
upstream commit in the file, and a CHANGELOG line at each sync.

Under the coordinator's markers, the Go ecosystem's "standard but not Google" set for web
services is short and stable: chi (router and middleware), `rs/cors`, `httpsnoop`,
`klauspost/compress`, `x/time/rate`, `go-oidc`, `jwx`, OpenTelemetry-Go, `pgx`.
`golang.org/x/*` sits between: Go-team maintained but outside the compatibility promise —
stdlib-adjacent, pinned. What fails and why it stays out: Gin, Echo, Fiber (own context
types); Viper (transitive graph); `go-playground/validator` (a preference DSL, a second
language to maintain); gorilla/mux (superseded by `ServeMux` 1.22).

## Placement

- Transport-generic, no infrastructure import → `go-web-sdk/middleware`.
- Collaborates with an infrastructure service (auth, tracing) → the infrastructure library's
  module, exposing a `func(http.Handler) http.Handler` over stdlib types so it is structurally
  a `web.Middleware` without importing the SDK (`concepts/service-middleware.md`, settled at
  the retrospective in this option's favor; the transport-vocabulary cost is accepted because
  every service hand-writing the adapter is the defect the SDK exists to remove).
- Copied third-party code → `go-web-sdk/middleware` with license header and upstream commit
  recorded in the file.

## Dependency-line statement

The SDK's declared line is "the standard library and go-core." Sourcing under this catalog is
a **stated enhancement** per the coordinator's rule: the repository README and `CLAUDE.md`
state which categories it admits (spec, threat model — cryptography stays out of this SDK
entirely) so the org's narrowing rule holds. A silent import no stated line covers is a
defect.

## Build findings folded into the session

- **Today a panic gives the client a dropped connection**: `RequestLogger` logs and re-panics
  into `net/http`'s recovery, which writes nothing — no 500 problem document exists on the
  panic path anywhere in the architecture.
- **The recoverer/logger ordering trap**: recoverer outermost writes its 500 after the
  logger's deferred status read (record says 200); recoverer innermost means the logger's
  `recover()` never fires (record loses the panic value). Neither ordering works with two
  independent recovery points — the recoverer stashes the panic on the shared wrapped writer
  and `logger.go`'s panic branch is rewritten in the same change, not left as a stale second
  recovery point.
- **Correlation**: the log record carries no request or trace id; when request ID lands,
  `RequestLogger` reads it, and `Problem.Instance` (today just `r.URL.Path`) is the natural
  place to surface it to clients. Request ID, recoverer, and logger correlation are one
  change, not three.
- The reference service's per-request gap this set closes: no handler-level deadline exists
  and the SDK default write timeout is 15 minutes; no body limit on read endpoints; no
  security headers; CORS arrives with the embedded client (`goals.v1.client`).

## Maintenance obligation

Everything hand-rolled is owned for the life of the standard. Each SDK release re-checks the
in-house middleware against the current Go release notes for `net/http` changes
(`http.ResponseController`, `ServeMux` pattern semantics, new `http.Server` fields) — a
scheduled release-checklist item, not an ad hoc one.
