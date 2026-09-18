# Changelog

All notable changes to the rate-limiting middleware
(`github.com/standards-lab/go-web-sdk/middleware/rate-limit`) are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers this sub-module
only; the base module keeps its own.

## [Unreleased]

### Added

- `ratelimit.New` and `ratelimit.Config` — per-client rate limiting over
  `github.com/go-chi/httprate`, keyed by the request's remote address with IPv6 clients bucketed
  by their /64, answering a request over the limit with a 429 problem document and httprate's
  `Retry-After` header. `Config` loads through go-core's config contract under the block
  `rate_limit`, defaulting to 300 requests per minute.

[Unreleased]: https://github.com/standards-lab/go-web-sdk/commits/HEAD/middleware/rate-limit
