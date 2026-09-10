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
  - the bind-then-serve server, declared as go-core's root-stage lifecycle service
  - route groups, modules, and the router
  - the probes, aggregating `lifecycle.Check` values
  - the problem writers
  - the `Middleware` type, with `Chain`
  - the read contract: `ParseQuery` splits a request's query string into paging and an ordered
    filter list with the bracket-operator grammar, as one `Query` under caller-supplied
    `Limits`; the `Page[T]` envelope carries a page of results
  - the request helpers, `IfMatch` and `DecodeJSON`
  - the `ErrorWriter`, which maps returned errors to problem responses. Its own error types map
    themselves and are sealed; consumer-supplied matchers decide the rest, with `Detail` and
    `Log` set at wiring
  - the error-returning handler adapter (`HandlerFunc`, `Handle`, `Group.HandleErr` under
    `Group.SetErrorWriter`), which never writes a second response

  Built.
- **middleware** holds the middleware implementations: the request logger. Built.
- **webtest** is the integration toolkit beside `web`. It provides:
  - the client a black-box suite drives a running service through, reading responses and
    problems as `web` writes them
  - the liveness observation a harness passes to go-core's `processtest`
  - the recorder helper for a handler test

  Built at `v1.data.sql.tasks.toolkit` (2026-09-07) from the reference service's harness.
- **Candidate direction** names what remains of the adapter story. `concepts/error-handling.md`
  covers the recorder exported and the logger rewritten onto it, the problem vocabulary with the
  readiness type hook, router 404/405 hooks, and the `ErrorLog` bridge.
  `concepts/middleware-sourcing.md` covers the middleware set and where service-collaborating
  middleware lives. The roadmap's `v1.web` goal sequences them.
