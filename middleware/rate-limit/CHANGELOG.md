# Changelog

All notable changes to the rate-limiting middleware
(`github.com/standards-lab/go-web-sdk/middleware/rate-limit`) are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers this sub-module
only; the base module keeps its own.

## [Unreleased]

## [v0.3.0] - 2026-10-09

### Changed

- **Breaking:** The `go-web-sdk` requirement is v0.15.1 and the `go-core` requirement v0.7.0.
  An importer still on go-core's `lifecycle.Service`, `Add`, or stages breaks, since the
  requirements pull go-core v0.6.0's lifecycle into its build.
- Built against `github.com/go-chi/httprate v0.16.1`, which changes the bucketing: an
  IPv4-mapped IPv6 address (`::ffff:192.0.2.1`) is now keyed as its IPv4 address and shares that
  address's counter. Under v0.16.0 it was reduced to the `::` /64 prefix, one counter every
  IPv4-mapped client shared.

## [v0.2.0] - 2026-09-30

### Changed

- Built against `github.com/standards-lab/go-web-sdk v0.13.0` and `github.com/standards-lab/go-core
  v0.5.0`; the requests override uses `config.SetFromEnv`.
- The godoc states each contract once, on its symbol, and the package comment lists every
  exported name.

### Removed

- **Breaking:** `ratelimit.NewEnv`. `Config.Env` carries the names `Finalize` composed; no
  workspace repository called it.

## [v0.1.1] - 2026-09-24

### Changed

- Built against `github.com/standards-lab/go-web-sdk v0.11.0` and `github.com/standards-lab/go-core
  v0.4.1`, the pins the workspace builds on. The middleware's own behavior is unchanged.

## [v0.1.0] - 2026-09-18

The first release of the rate-limiting middleware, against
`github.com/standards-lab/go-web-sdk v0.9.0`.

### Added

- `ratelimit.New` and `ratelimit.Config` — per-client rate limiting over
  `github.com/go-chi/httprate`, keyed by the request's remote address with IPv6 clients bucketed
  by their /64, answering a request over the limit with a 429 problem document and httprate's
  `Retry-After` header. `Config` loads through go-core's config contract under the block
  `rate_limit`, defaulting to 300 requests per minute.

[Unreleased]: https://github.com/standards-lab/go-web-sdk/compare/middleware/rate-limit/v0.3.0...HEAD
[v0.3.0]: https://github.com/standards-lab/go-web-sdk/compare/middleware/rate-limit/v0.2.0...middleware/rate-limit/v0.3.0
[v0.2.0]: https://github.com/standards-lab/go-web-sdk/compare/middleware/rate-limit/v0.1.1...middleware/rate-limit/v0.2.0
[v0.1.1]: https://github.com/standards-lab/go-web-sdk/compare/middleware/rate-limit/v0.1.0...middleware/rate-limit/v0.1.1
[v0.1.0]: https://github.com/standards-lab/go-web-sdk/releases/tag/middleware/rate-limit/v0.1.0
