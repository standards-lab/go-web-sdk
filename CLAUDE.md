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

- **Module layout** — one Go module rooted at `github.com/standards-lab/go-web-sdk`. The `web`
  package occupies the module root, and `middleware` and `webtest` are its sub-packages. There
  are no sub-modules.
- **Dependencies** — the standard library and go-core, per the Go Elemental dependency line.
- **Releases, CI, tests, tasks** — per the Go Elemental standard principles in the architecture repository
  (root `v<semver>` tags from `CHANGELOG.md`, hermetic `httptest`/port-0 tests with the
  recorder helper from `webtest`, mise tasks).
- **Public repo.** The module resolves through the public Go proxy; CI carries no private-module
  config.
