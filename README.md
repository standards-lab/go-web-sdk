# go-web-sdk

Go web SDK for Standards Lab: the HTTP server and its configuration, routing, RFC 9457 problem
responses, the liveness and readiness probes, and middleware.

`github.com/standards-lab/go-web-sdk` is a single Go module; the `web` package occupies the module
root, and `middleware` is its one sub-package.

## Target standard

`go-web-sdk` is the application SDK for web services of `go-minimal`, the minimal-dependency Go
standard. Its repository-level principles:

- The module depends on the standard library and `go-core`, and takes at most packages as idiomatic
  and stable as the standard library. Vendor SDKs never enter it.
- Its standard tier is RFC 9110 and RFC 9457 over the stdlib `net/http` transport, and it has no
  providers: nothing changes on a provider swap because there is nothing to swap.
- `web` is one cohesive package; `middleware` is the one sub-package, holding the middleware
  implementations while the `Middleware` type and `Chain` stay in `web`, where routing consumes
  them.

## Packages

- `web` — the HTTP layer: a `net/http` server wired for go-core's lifecycle, its configuration
  block, route groups, modules, and the router, RFC 9457 problem responses, a JSON writer, the
  `/healthz` and `/readyz` probes, and the middleware primitives.
- `middleware` — the middleware implementations: the request logger today, with the rest of the set
  arriving as consumers demand them.

## Development

Tasks run through [mise](https://mise.jdx.dev):

```
mise run test
```

## License

[Apache License 2.0](LICENSE).
