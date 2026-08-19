# Middleware

The design of the middleware layer. The code and the two `doc.go` files are authoritative for the
API; this note records the reasoning.

## The type in `web`, the implementations in `middleware`

`Middleware` is `func(http.Handler) http.Handler` — the signature the ecosystem already uses — and
`Chain` composes a set of them in argument order, the first argument seeing the request first. Both
live in `web` because the routing layer consumes them: a group's stack, a route's wrappers, and the
router's dispatch chain are all `Middleware` values.

The implementations live in the `middleware` package. Middleware is the SDK's growth area — CORS, a
recovery handler, a request ID are all foreseeable — so the instances get a package to grow in
without enlarging `web`, and the import runs `middleware → web` only. The split is by growth, not by
topic; see `design/topology-and-naming.md`.

## Middleware belongs to the transport

A transport-agnostic capability does not define HTTP middleware. The request logger lives here and
writes through a standard `*slog.Logger` rather than living in a logging package and returning an
HTTP type; the same rule places every future enforcement point in this module or its consumers.
The alternative inverts the dependency direction — a worker that wants a logger would compile
`net/http` to get one — and the record a request logger emits is HTTP vocabulary in any case. The
capability supplies the collaborator; the transport supplies the middleware that consumes it. Where
middleware that collaborates with an external capability should live is open in
`concepts/capability-middleware.md`.

## The request logger

One record per request — method, path, status, duration, remote address — at info level. A
successful probe request logs at debug, because an orchestrator requests `/healthz` and `/readyz`
every few seconds forever and that heartbeat would otherwise dominate production logs; a failing
probe stays at info, since readiness flapping is exactly what an operator greps for. A panicking
handler logs its record at error with the panic value attached, then the panic continues to
net/http's recovery. Beyond the probe carve-out the middleware does not judge status codes: whether
a 5xx was the application's own failure belongs to the error mapping, not here.

## The ResponseWriter wrapper

Wrapping the `ResponseWriter` to record the status is where request loggers accumulate defects, and
the wrapper is written against two known ones: swallowing a second `WriteHeader` instead of
delegating it, which hides the standard library's superfluous-header warning, and omitting `Unwrap`,
which silently costs a handler flushing and hijacking. The wrapper here records the first status,
always delegates, implements `Unwrap` so `http.ResponseController` reaches through it, and delegates
`io.ReaderFrom` so a handler serving files keeps the zero-copy path. Seeding the recorded status
with 200 covers the handler that writes a body without calling `WriteHeader`, which removes any need
to intercept `Write`. The duration attribute is a `slog.Duration`, rendered by the JSON handler as
nanoseconds; a `duration_ms` float would suit dashboards better and waits for an observability
consumer to ask.
