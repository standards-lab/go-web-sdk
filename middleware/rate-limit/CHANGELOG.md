# Changelog

All notable changes to the rate-limiting middleware
(`github.com/standards-lab/go-web-sdk/middleware/rate-limit`) are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers this sub-module
only; the base module keeps its own.

## [Unreleased]

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

[Unreleased]: https://github.com/standards-lab/go-web-sdk/compare/middleware/rate-limit/v0.1.1...HEAD
[v0.1.1]: https://github.com/standards-lab/go-web-sdk/compare/middleware/rate-limit/v0.1.0...middleware/rate-limit/v0.1.1
[v0.1.0]: https://github.com/standards-lab/go-web-sdk/releases/tag/middleware/rate-limit/v0.1.0
