# go-web-sdk

go-web-sdk is the Application SDK for web services of Standards Lab's Go Elemental standard. It
provides:

- the HTTP server and its configuration
- routing
- serving an embedded client application
- RFC 9457 problem responses
- the liveness and readiness probes
- middleware
- resumable uploads

`github.com/standards-lab/go-web-sdk` is a base Go module with the `web` package at its root and
`middleware`, `webtest`, `upload`, and `upload/uploadtest` as sub-packages. A middleware that
needs a third-party library is a sub-module of its own under `middleware/`, released on its own
tag, so a service that does not use it never compiles its dependencies.

## Standard

[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md)
is the minimal-dependency Go standard. This README and each package's `doc.go` document the
repository. The pages under `docs/` hold the reasoning behind the unexported machinery: the
[client handler](docs/client.md), [routing misses](docs/routing.md),
[problems](docs/problems.md), [requests and responses](docs/requests-and-responses.md), the
[server and service](docs/server.md), and the [middleware](docs/middleware.md).

The repository-level principles below enhance the standard's:

- The base module depends on the standard library and `go-core`, and takes at most packages as
  idiomatic and stable as the standard library. Vendor SDKs never enter it.
- A middleware sub-module states its own line. It admits a sourced dependency in one category —
  a specification surface, or a threat model whose corner cases are the library's product —
  under the
  [markers for a standard library](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/principles/dependencies.md#sourcing);
  cryptography stays out of this SDK entirely. `middleware/rate-limit` takes
  `github.com/go-chi/httprate` under the threat-model category: per-key limiting with eviction,
  whose failure mode is a memory-growth denial of service rather than a wrong answer a test
  would catch.
- Its standard tier is RFC 9110 and RFC 9457 over the stdlib `net/http` transport, with
  draft-ietf-httpbis-resumable-upload-16, RFC 9530 digests, and RFC 9651 Structured Fields for
  uploads, and it has no providers: nothing changes on a provider swap because there is nothing
  to swap.
- `web` is one cohesive package. `middleware` is the sub-package holding the hand-rolled
  middleware implementations, while the `Middleware` type and `Chain` stay in `web`, where
  routing consumes them. `webtest` is the integration toolkit a service's suite drives it
  through.

## Packages

- `web` is the HTTP layer. It provides:
  - a `net/http` server wired for go-core's lifecycle, with its configuration block, route
    groups, modules, and the router. Its read and write timeouts are tight, so a route that moves
    a large body sets its own deadlines through `Transfer`, from the body's size and the slowest
    pace a client is allowed
  - the service runner: `web.Service{...}.Run(ctx, os.Stderr)` builds the service's graph, runs
    it under go-core's lifecycle Coordinator until the context ends, logs the server ready with
    its bound address, and returns the process exit code
  - RFC 9457 problem responses and a JSON writer
  - the read contract: a request's paging, sort, cursor, and filters parsed as one `Query`, and
    the `Page` envelope with its total, `more`, and `next`
  - the request helpers: the `If-Match` precondition, a strict JSON body, a UUID path value, and a
    raw upload accepted by its declared type and length
  - the proxied object response, with its validators and a 304 on a matching `If-None-Match`
  - `NewClient`, the handler that serves a client application from its embedded Vite build: the
    HTML shell for every client route, with the client's configuration written into it, and the
    build's files, those under the manifest's directories cached for a year and the rest
    revalidated against an ETag
  - the `/healthz` and `/readyz` probes
  - the middleware primitives
- `middleware` holds the hand-rolled middleware implementations: the request logger, the
  recoverer, `RequestID`, `Timeout`, `Headers`, `Maybe`, `ContentType`, and `BodyLimit`. A
  middleware with a third-party dependency is a sub-module of its own under `middleware/`;
  `middleware/rate-limit` is the first.
- `webtest` is the integration toolkit. It provides:
  - the client a black-box suite drives a running service through, sending JSON or raw bodies
    and reading responses and RFC 9457 problems as `web` writes them
  - the liveness observation a harness waits on
- `upload` serves and sends resumable uploads as draft-ietf-httpbis-resumable-upload-16 defines
  them, each chunk and the whole file checked against an RFC 9530 SHA-256 digest. It provides:
  - the server handler. A service mounts it by nesting `upload.Handler{...}.Routes(prefix)` in
    one of its groups, supplying the store, the transfer limit, the upload lifetime, and the
    `Complete` function that takes the finished file
  - the Go client, which creates an upload resource and sends a file in chunks, resuming from the
    server's offset after an interruption
  - the `Store` interface, in standard library types only, and `MemoryStore`, which serves a
    single process. A service with more than one replica writes its own `Store` over storage
    its replicas share, and its tests run `upload/uploadtest`, the conformance suite, against it
  - the digest and Structured Field helpers the draft's fields use

  Out of scope: a store backed by blob storage, interop with tusd, 104 progress responses during
  an append, and digest algorithms other than SHA-256.

## Development

Tasks run through [mise](https://mise.jdx.dev):

```
mise run check      # build, vet, format, fix, tidy, test, and lint every module, then lint-context and lint-references; writes nothing
mise run currency   # report requirements, Go, tools, actions, and the upload draft behind their latest
mise run upgrade    # upgrade every module's go directive and requirements, and the tools, to their latest
```

## License

[Apache License 2.0](LICENSE).
