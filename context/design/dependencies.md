# Dependencies

What the module may depend on, and what it imports from `go-core`.

## The dependency rule

The module depends on the standard library and `go-core`, and may take at most packages as
idiomatic and stable as the standard library itself (`golang.org/x/…`, `google/uuid`, and the
like). Vendor SDKs never enter it. Today the only requirement is `go-core`; the HTTP transport is
the stdlib's `net/http`.

The SDK has no providers. Its standard tier is exactly HTTP's common standard — RFC 9110 for the
protocol, RFC 9457 for problem responses — over the one transport every Go program already
compiles, so there is no native tier and no provider sub-module: nothing changes on a provider swap
because there is nothing to swap.

## What the module imports from go-core

- **config** — `config.Duration` for the timeout fields, `config.EnvName` to compose the
  environment override names, and `config.SetDurationFromEnv` in `Finalize`. `web.Config`
  implements the Merge and Finalize contract of go-core's `config` package, so it loads as part of
  an application's configuration.
- **lifecycle** — the `lifecycle.ReadinessChecker` interface alone, consumed by `Readiness`.
  `Server.Start` and `Server.Shutdown` match the coordinator's hook signature and `Server.Err` is a
  monitorable source, but that integration is structural: registering them requires no lifecycle
  import here.

## What it never imports

`logging` never enters the module. The request logger writes through a standard `*slog.Logger`: a
middleware belongs to the transport that consumes it, and the record it emits is HTTP vocabulary,
but the logger itself is an ordinary dependency the composition root constructs. See
`design/middleware.md`.
