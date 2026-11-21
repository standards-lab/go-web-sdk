# go-web-sdk

go-web-sdk is the Application SDK for web services of Go Elemental, the Standards Lab
organization's Go implementation of the Elemental Architecture. It provides the HTTP server and
its configuration, routing, serving an embedded client application, RFC 9457 problem responses,
the liveness and readiness probes, and middleware.

The README, the pages under `docs/`, and each package's `doc.go` document this repository. The
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md) standard states the principles it follows.
This context records only working knowledge the code and its documentation do not express.

Each package's comment in its `doc.go` is the authoritative description of its API; the
capability map only points to it. An unbuilt capability gains written detail when a session is
about to build it.

## Capability map

- **web** (built): the HTTP layer ([`doc.go`](../doc.go)), with the server (`NewServer`,
  `Config`), routing (`Group`, `NewModule`, `Router`), the problem model (`Problem`, `ErrorWriter`,
  `Handle`), the read contract (`ParseQuery`, `NewPage`), the request helpers (`IfMatch`,
  `DecodeJSON`, `ReadUpload`, `PathUUID`), the object proxy (`WriteObject`, `Attachment`), the
  client handler (`NewClient`, `ClientOptions`, `ManifestPath`, `ConfigElementID`), the probes
  (`RegisterHealth`, `Doctor`), `Recorder`, the `Middleware` type with `Chain`, and the service
  runner (`Service`). The client handler is ported from spike-html-apps' `sdk/web.go` (2883d78)
  ([github.com/JaimeStill/spike-html-apps](https://github.com/JaimeStill/spike-html-apps),
  archived); the v0.18.0 CHANGELOG entry says how it differs.
- **middleware** (built): the hand-rolled middleware, written in this repository, and their chain
  order ([`middleware/doc.go`](../middleware/doc.go)). `middleware/rate-limit`, built over a
  third-party library, is the first sourced sub-module.
- **webtest** (built): the integration toolkit, with the black-box client and the liveness
  observation ([`webtest/doc.go`](../webtest/doc.go)).
- **Error handler extension points** (planned): the two deferred ones
  ([error-handling.md](error-handling.md)).
- **Unbuilt middleware** (planned): one hand-rolled and three sourced
  ([middleware-sourcing.md](middleware-sourcing.md)).
