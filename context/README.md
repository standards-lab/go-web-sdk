# go-web-sdk

go-web-sdk is the Application SDK for web services of Go Elemental, the Standards Lab
organization's Go implementation of the Elemental Architecture. It provides the HTTP server and
its configuration, routing, RFC 9457 problem responses, the liveness and readiness probes, and
middleware.

The README and each package's `doc.go` document this repository. The
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md) standard states the principles it follows.
This context records only working knowledge the code and the README do not express.

## Capability map

The code and each package's `doc.go` are authoritative for what is built. An unbuilt capability gains written detail when a session is about to
build it.

- **web** is the HTTP layer. It provides:
  - the bind-then-serve server, declared as go-core's root-stage lifecycle service; `Server.Log`
    bridges `http.Server.ErrorLog` to a `*slog.Logger`
  - route groups, modules, and the router; a request matching no route, or the wrong method,
    answers with an RFC 9457 problem document (`Allow` preserved on a 405) rather than
    `ServeMux`'s plain text, overridable per router (`Router.SetNotFound`/`SetMethodNotAllowed`)
    or per module (the same pair on `Group`, set on the group passed to `NewModule`)
  - the probes, aggregating `lifecycle.Check` values
  - `Config`'s per-block environment segment: `FinalizeBlock` finalizes under a caller-named
    block instead of `"server"`, so a second `Config` composes under the same prefix without
    colliding; `MaxHeaderBytes` carries no SDK default and leaves `net/http`'s own in place when
    unset
  - the request-id seam: `WithRequestID`/`RequestIDFrom` carry a request's correlation id on its
    context, and `Problem.WriteFor` surfaces it as the `request_id` extension member
  - the problem writers
  - the `Middleware` type, with `Chain`
  - `Recorder`, the wrapped-writer that records whether a response has been committed and with
    what status, and `WrapWriter`, its idempotent constructor; `Handle` and the middleware
    package's `RequestLogger` and `Recoverer` share one per request through it
  - the read contract: `ParseQuery` splits a request's query string into paging and an ordered
    filter list with the bracket-operator grammar, as one `Query` under caller-supplied
    `Limits`; the `Page[T]` envelope carries a page of results
  - the request helpers, `IfMatch` and `DecodeJSON`
  - the `ErrorWriter`, which maps returned errors to problem responses through a composed
    `ProblemMatcher` list. Its own error types map themselves and are sealed and checked first;
    consumer-supplied matchers decide the rest and may carry their own type, title, and
    extension members via `Problem`, not status alone. `Detail` and `Log` are set at wiring
  - `Problem`'s `Extras` and its `MarshalJSON`/`UnmarshalJSON` pair, so a document with
    extension members round-trips; `WriteFor` sets `Instance` from the request path; `Error`
    renders the status, title, and detail as one line, so a `Problem` serves directly as the
    `error` a caller receives, with no wrapper type needed to bridge it to the interface
  - the error-returning handler adapter (`HandlerFunc`, `Handle`, `Group.HandleErr` under
    `Group.SetErrorWriter`), which never writes a second response; a write failure and a
    post-commit error are both logged rather than swallowed

  Built.
- **middleware** holds the middleware implementations: the request logger (its attributes named
  by OpenTelemetry's semantic conventions, plus `http.route` and `request_id` when present), the
  recoverer, the chain's one recovery point, `RequestID` (a correlation id with a source seam for
  a tracer), `Timeout`, `Headers`, `Maybe`, `ContentType`, and `BodyLimit`. Built.
- **webtest** is the integration toolkit beside `web`. It provides:
  - the client a black-box suite drives a running service through, reading responses and
    problems as `web` writes them
  - the liveness observation a harness passes to go-core's `processtest`
  - the recorder helper for a handler test

  Built at `v1.data.sql.tasks.toolkit` (2026-09-07) from the reference service's harness.
- **Candidate direction**: `error-handling.md` holds the error handler's two deferred extension
  points, and `middleware-sourcing.md` the rest of the middleware set.
