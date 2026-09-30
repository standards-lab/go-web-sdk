# go-web-sdk

go-web-sdk is the Application SDK for web services of Go Elemental, the Standards Lab
organization's Go implementation of the Elemental Architecture. It provides the HTTP server and
its configuration, routing, RFC 9457 problem responses, the liveness and readiness probes, and
middleware.

The README and each package's `doc.go` document this repository. The
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md) standard states the principles it follows.
This context records only working knowledge the code and the README do not express.

## Capability map

Each package's `doc.go` and its symbols' godoc are authoritative for what is built; this map only
points at them. An unbuilt capability gains written detail when a session is about to build it.

- **web** (`doc.go`) is the HTTP layer: the server (`NewServer`, `Config`), routing (`Group`,
  `NewModule`, `Router`), the problem model (`Problem`, `ErrorWriter`, `Handle`), the read
  contract (`ParseQuery`, `NewPage`), the request helpers (`IfMatch`, `DecodeJSON`,
  `ReadUpload`, `PathUUID`), the object proxy (`WriteObject`, `Attachment`), the probes
  (`RegisterHealth`), `Recorder`, and the `Middleware` type with `Chain`. Built.
- **middleware** (`middleware/doc.go`) holds the hand-rolled middleware and their chain order;
  `middleware/rate-limit` is the first sourced sub-module. Built.
- **webtest** (`webtest/doc.go`) is the integration toolkit: the black-box client and the
  liveness observation. Built.
- **Candidate direction**: `error-handling.md` records the error handler's two deferred
  extension points, and `middleware-sourcing.md` records the middleware still unbuilt.
