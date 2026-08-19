# Changelog

All notable changes to `github.com/standards-lab/go-web-sdk` are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.1.0] - 2026-08-19

The first release of the web SDK: the `web` and `middleware` packages. The module depends on the
standard library and `github.com/standards-lab/go-core v0.1.0`.

### Added

- `web` — the HTTP layer. `NewServer` wraps an `http.Server` built from a finalized `Config` and a
  caller-composed handler; `Start` binds on the calling goroutine and only then serves in the
  background, so a bind failure is a returned error, and `Start`, `Shutdown`, and `Err` match
  go-core's lifecycle hook and monitor contracts, so a composition root wires the server as bare
  method values. The tri-state `Config` (nil is unset and takes the default; an explicit zero
  survives the load) implements the Merge and Finalize contract of go-core's `config` package.
  `Group`, `Module`, and `Router` compose the route tree: `NewModule` compiles a group tree once,
  with middleware ordered root to leaf then route, and seals it, so late mutation panics;
  the router dispatches by longest-prefix match on segment boundaries and falls back to a native
  `ServeMux`, which satisfies `Mounter`, so `RegisterHealth` mounts `/healthz` and `/readyz`
  outside every module's middleware. Error responses are RFC 9457 problem documents (`Problem`,
  `WriteProblem`, `WriteProblemWith`; every emitted problem is `about:blank`, with type URIs left
  to the consumer), `WriteJSON` writes success bodies, and `Middleware` with `Chain` composes
  handler wrappers in argument order.
- `middleware` — the middleware implementations. `RequestLogger` emits one record per request
  through a caller-supplied `*slog.Logger`, demotes a successful probe to debug, logs a panicking
  handler at error before the panic continues, and wraps the `ResponseWriter` so the recorded
  status, `http.ResponseController`, and `io.ReaderFrom` all keep working.
