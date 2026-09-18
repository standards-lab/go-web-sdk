# The middleware set: hand-roll or source

`v1.web.tasks.middleware`'s close landed the hand-rolled catalog in full except path hygiene, and
the org-wide rule and the "standard library" markers live at the coordinator
(`standards-lab/context/design/dependency-sourcing.md`); this note carries what remains: the one
deferred hand-rolled item, and the sourced set `goals.v1.middleware` has yet to adopt beyond rate
limiting, built as `middleware/rate-limit`.

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
| Rate limiting | A single-process token bucket is easy; per-key with eviction, or distributed, is not. | `github.com/go-chi/httprate`, a sliding-window counter with its own per-key eviction. `golang.org/x/time/rate`'s bucket does not compose under it and adds no algorithmic benefit paired with it, so it is not a dependency here. |
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

## Rate limiting's keying

`middleware/rate-limit` keys on the request's `RemoteAddr` — the direct TCP peer, not a forwarded
header — through a key function it supplies to `httprate.LimitBy`. Keying on `X-Forwarded-For`
would reproduce the exact attacker-controlled-header problem the real-IP row above exists to
solve; `RemoteAddr` is safe without a trusted-proxy configuration, so rate limiting needed no
real-client-IP prerequisite to land.

When real client IP lands, rate limiting's key function is the seam it replaces: the key source
moves from `RemoteAddr` to that middleware's output. That is a forward trigger for whichever
session builds real client IP, not further work for rate limiting itself.

## What triggers real client IP and compression

Neither has a recorded trigger; both wait on a consumer, not a version or a sequencing slot.

- **Real client IP** becomes necessary once something needs a client identity it can trust: a
  reverse proxy or load balancer sits in front of the service (the deployment goal, unscoped), or
  a consumer other than logging needs one — geo-blocking, per-client abuse detection, an audit
  trail keyed on IP. Until then, `RemoteAddr` already serves the two consumers that exist:
  `RequestLogger`'s and `Recoverer`'s `client.address` field, and rate limiting's key (above).
- **Compression** becomes necessary once a response is large enough for the wrapping and content
  negotiation to pay for themselves — a domain endpoint returning many rows (the data layer's
  paginated lists, once domains beyond `organization` land) or a bulk admin export. No such
  response exists yet; the service has no large-payload endpoint to compress.

A session that lands either writes its task under `goals.v1.middleware.tasks` in the roadmap at
the point its trigger fires, the same way rate limiting's did.

## Placement

- Transport-generic, no third-party dependency → `go-web-sdk/middleware`, beside the hand-rolled
  set.
- Transport-generic, with a third-party dependency → a sub-module of its own at
  `go-web-sdk/middleware/<concern>`, with its own `go.mod`, `doc.go`, tests, and `CHANGELOG.md`,
  releasing on a `middleware/<concern>/v<semver>` tag. It requires the base module by version, the
  base module never imports it, and a service takes it as a second `require` line alongside the
  SDK. `middleware/rate-limit` is the first.
- Collaborates with an infrastructure service (auth, tracing) → the infrastructure library's
  module, exposing a `func(http.Handler) http.Handler` over stdlib types so it is structurally a
  `web.Middleware` without importing the SDK.
- Copied third-party code → not a placement here: the coordinator's rule admits a sourced library
  as a declared dependency and rejects vendoring it by hand.

The SDK's dependency line and its sub-module admission category are the README's; this note
does not restate them.

## Maintenance obligation

The coordinator's obligation on hand-rolled code applies; for this SDK the release re-check is
against `net/http` changes (`http.ResponseController`, `ServeMux` pattern semantics, new
`http.Server` fields).
