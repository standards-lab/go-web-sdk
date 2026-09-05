# Changelog

All notable changes to `github.com/standards-lab/go-web-sdk` are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `web`: `IfMatch` reads a request's version precondition from the If-Match header, exactly one
  strong entity-tag holding an integer version; a missing or malformed header is a
  `*PreconditionError`, which `ErrorWriter` maps to a 428 or a 400 built in. Promoted from the
  reference service's sdk package.
- `web`: `DecodeJSON` reads a request body strictly as one JSON value of the given type: bounded
  at the caller's limit, unknown fields rejected, nothing after the first value. A rejected body
  is a `*BodyError`, which `ErrorWriter` maps to a 413 when the body is over its limit and a
  400 otherwise. Promoted from the reference service's per-domain `decode`, which answered 400
  for an oversized body.
- `web`: `ErrorWriter.Detail` adds statuses whose problems carry the error text as their
  detail member, for a surface whose clients need the reason. The built-in set is 400, 413,
  and 428.

### Changed

- `web`: `ErrorWriter.Write` decides the detail member from the writer's detail set rather
  than from the single status 400.

## [v0.5.0] - 2026-08-28

The read parse consolidated and the error-to-problem mapping, promoted from the reference
service's sdk package with both slices of evidence in hand. The SDK maps its own vocabulary
and nothing else's: HTTP status policy for infrastructure errors is declared by the consumer
through matchers, keeping the application SDK and the infrastructure libraries peers on
go-core.

### Added

- `web`: `ParseQuery` parses a read request's query string in full into the new `Query` —
  page, size, and sort under the caller's `Limits`, and every remaining parameter as the
  filter set — so a handler cannot parse the paging parameters and forget to strip them from
  the filters. A rejected parameter is a `*QueryError`.
- `web`: `ErrorWriter` turns a handler's returned error into an RFC 9457 problem response
  through a composed `StatusMatcher` list: `*QueryError` → 400 built in, the consumer's
  matchers decide the rest in order, first match wins, 500 the fallback. The detail member
  carries the error text only on a 400; no internal error's text reaches the wire.

### Changed

- `web`: `NewPage` takes the read's `Query` in place of the removed `Directives`.

### Removed

- `web`: `ParseDirectives`, `Directives`, and `DirectiveError`, absorbed by `ParseQuery`,
  `Query`, and `QueryError` — one way to parse a read, one flat result.

## [v0.4.0] - 2026-08-26

The HTTP side of paginated reads: the request directives parsed from the query string, and the
success envelope. The contract carries no storage detail — directive field names are lexical
here, and whether one names a readable field is the data layer's check.

### Added

- `web`: `ParseDirectives` reads `page`, `size`, and `sort` (`sort=name,-code`, honored across
  repeated parameters) into the new `Directives` and `Sort` types, under a caller-supplied
  `Limits` — the SDK holds no policy numbers of its own, and invalid limits panic as a wiring
  mistake. A malformed or out-of-bounds parameter returns a `*DirectiveError` for the handler
  to map to its own 400 problem, consistent with the SDK minting no problem types. `Page[T]` is
  the `items`/`page`/`size`/`total` success envelope, assembled by `NewPage` — nil items
  marshal as `[]`, never `null` — and written with `WriteJSON`.

## [v0.3.1] - 2026-08-24

### Changed

- The go-core pin moves to v0.3.0, which adds the `process` package and builds on Go 1.27.
  Nothing in the SDK uses the new package; the pin is the committed steady state for consumers
  building on this release.

## [v0.3.0] - 2026-08-21

`RegisterHealth` takes the coordinator directly and queries it live on every request, instead of a
fixed slice of checks captured once at registration: a service the coordinator gains afterward now
appears on the next probe instead of vanishing from it.

### Changed

- `web`: `RegisterHealth(m Mounter, lc *lifecycle.Coordinator)` replaces `RegisterHealth(m Mounter,
  checks ...lifecycle.Check)`. It prepends the coordinator itself, under the fixed name
  `"lifecycle"`, to `lc.Checks()`, evaluated fresh on every request. `Readiness` is unchanged, and
  still takes a caller-supplied check list directly.

## [v0.2.0] - 2026-08-21

The named readiness check moves to its process-level home: `Readiness` and `RegisterHealth` now
consume go-core's `lifecycle.Check`, and `web.Check` is removed.

### Changed

- `web`: `Readiness` and `RegisterHealth` consume `lifecycle.Check` values. The aggregation, the
  nil-checker rule, and the 503 problem document are unchanged. The lifecycle-wiring example in
  the package documentation declares the server as a `lifecycle.Service` in `StageRoot` —
  started after every numbered stage, drained first — replacing the hook wiring.
- The module requires `github.com/standards-lab/go-core v0.2.0` and Go 1.27.

### Removed

- `web.Check`, replaced by `lifecycle.Check`, moved verbatim. The field names are unchanged, so
  a call site updates by qualifying the type.

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

[Unreleased]: https://github.com/standards-lab/go-web-sdk/compare/v0.5.0...HEAD
[v0.5.0]: https://github.com/standards-lab/go-web-sdk/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/standards-lab/go-web-sdk/compare/v0.3.1...v0.4.0
[v0.3.1]: https://github.com/standards-lab/go-web-sdk/compare/v0.3.0...v0.3.1
[v0.3.0]: https://github.com/standards-lab/go-web-sdk/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/standards-lab/go-web-sdk/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/standards-lab/go-web-sdk/releases/tag/v0.1.0
