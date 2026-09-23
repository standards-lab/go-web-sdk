# Middleware sourcing

The organization's rule decides whether a middleware is hand-rolled or sourced
(`standards-lab/context/design/dependency-sourcing.md`). The README states where each kind lives.
This note holds what remains of the middleware set: one hand-rolled item and three sourced ones.
CORS is planned under `v1.middleware`; real client IP and compression wait in the backlog.

## Path hygiene

Cleaning the path and stripping a trailing slash stay unbuilt. `net/http.ServeMux` already
redirects an unclean path and handles a trailing slash, and the condition for adding it, a real
client that needs behavior `ServeMux` does not give, has not arisen.

## Sourced middleware

| Concern | Why not hand-roll | Library |
|---|---|---|
| CORS | Preflight rules, `Vary` correctness, credentialed-origin restrictions, `Access-Control-Max-Age`, and Private Network Access headers. Wrong answers look right. | `github.com/rs/cors` |
| Real client IP | The parse is short, but `X-Forwarded-For` is attacker-controlled unless the trusted proxies are configured. | Implemented against a configured trusted-proxy list, with `chi/v5/middleware.RealIP` as the shape reference; Adam Pritchard's "The perils of the 'real' client IP" is the threat model. |
| Compression | `ResponseWriter` wrapping, content negotiation, skipping compressed media types, writer pooling, and preserving `Flusher`. | `github.com/klauspost/compress/gzhttp` |

Token verification and tracing are sourced too, in the infrastructure libraries rather than this
SDK: `github.com/coreos/go-oidc/v3` for OIDC against Keycloak, and `otelhttp` in
go-observability.

## Triggers

Each waits on a consumer. The session that meets the trigger for a backlog item moves it to a
task under `v1.middleware`.

- **Real client IP**: a reverse proxy or load balancer in front of the service, or a consumer
  that needs a trusted client identity, such as geo-blocking, abuse detection, or an audit trail
  keyed on IP. Until then `RemoteAddr` serves the request logger, the recoverer, and
  `middleware/rate-limit`. When it lands, rate limiting's key moves from `RemoteAddr` to this
  middleware's output.
- **Compression**: a response large enough to pay for the wrapping, such as a paginated list over
  many rows or a bulk export.
- **CORS**: the client layer, `v1.client`.

## Maintenance

Each release re-checks the hand-rolled set against `net/http` changes: `http.ResponseController`,
`ServeMux` pattern semantics, and new `http.Server` fields.
