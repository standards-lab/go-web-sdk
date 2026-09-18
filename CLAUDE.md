# go-web-sdk

go-web-sdk is the Application SDK for web services of Go Elemental, the Standards Lab
organization's Go implementation of the Elemental Architecture. It provides the HTTP server and
its configuration, routing, RFC 9457 problem responses, the liveness and readiness probes, and
middleware. The repository is managed with the marathon workflow; start from
`context/README.md`.

## Documentation lives in the repository

This repository documents its own implementation: the README states its place in the standard
and the principles it enhances, and each package's `doc.go` is the authority for its API. The
organization's [architecture repository](https://github.com/standards-lab/architecture) states the principles this repository follows
and holds nothing a reader can infer from this source. `context/` records only working
knowledge the code and the README do not express; do not restate documented design here. A
change that alters documented behavior updates the README and the package documentation in
the same effort, and a design note that generalizes past this repository is promoted to the
architecture repository through its `context/`.

## Repository specifics

- **Module layout** — one base module rooted at `github.com/standards-lab/go-web-sdk`, holding
  the `web` package at its root with `middleware` and `webtest` beside it. A middleware that
  needs a third-party library carries it in a sub-module of its own under `middleware/`, with
  its own `go.mod` and its own release tag — `middleware/rate-limit` today. The base module
  never imports one.
- **Dependencies** — the base module takes the standard library and go-core, per the Go Elemental
  dependency line. A sourced dependency enters only through a middleware sub-module's `go.mod`:
  `httprate` through `middleware/rate-limit`.
- **Local development** — development uses the committed root `go.work`. Pinned `require`
  versions are the committed steady state; a `replace` directive is only a transient bridge
  while a sub-module builds against unreleased base changes.
- **Releases, CI, tests, tasks** — per the Go Elemental standard principles in the architecture
  repository (base `v<semver>` tags and `middleware/<name>/v<semver>` tags from each module's own
  `CHANGELOG.md`, a per-module CI matrix, hermetic `httptest`/port-0 tests with the recorder
  helper from `webtest`, mise tasks looping over the modules).
- **Public repo.** The module resolves through the public Go proxy; CI carries no private-module
  config.
