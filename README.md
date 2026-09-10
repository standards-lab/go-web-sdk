# go-web-sdk

go-web-sdk is the Application SDK for web services of Standards Lab's Go Elemental standard. It
provides:

- the HTTP server and its configuration
- routing
- RFC 9457 problem responses
- the liveness and readiness probes
- middleware

`github.com/standards-lab/go-web-sdk` is a single Go module. The `web` package occupies the
module root, and `middleware` and `webtest` are its sub-packages.

## Standard

`go-web-sdk` is the Application SDK for web services of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md), the
minimal-dependency Go standard. This README and each package's `doc.go` document the
repository; the standard's principles it enhances are stated below.
Its repository-level principles:

- The module depends on the standard library and `go-core`, and takes at most packages as idiomatic
  and stable as the standard library. Vendor SDKs never enter it.
- Its standard tier is RFC 9110 and RFC 9457 over the stdlib `net/http` transport, and it has no
  providers: nothing changes on a provider swap because there is nothing to swap.
- `web` is one cohesive package. `middleware` is the sub-package holding the middleware
  implementations, while the `Middleware` type and `Chain` stay in `web`, where routing consumes
  them. `webtest` is the integration toolkit a service's suite drives it through.

## Packages

- `web` is the HTTP layer. It provides:
  - a `net/http` server wired for go-core's lifecycle, with its configuration block, route
    groups, modules, and the router
  - RFC 9457 problem responses and a JSON writer
  - the `/healthz` and `/readyz` probes
  - the middleware primitives
- `middleware` holds the middleware implementations: the request logger today, with the rest of
  the set arriving as consumers demand them.
- `webtest` is the integration toolkit. It provides:
  - the client a black-box suite drives a running service through, reading responses and
    RFC 9457 problems as `web` writes them
  - the liveness observation a harness waits on
  - the recorder helper for a handler test

## Development

Tasks run through [mise](https://mise.jdx.dev):

```
mise run test
```

## License

[Apache License 2.0](LICENSE).
