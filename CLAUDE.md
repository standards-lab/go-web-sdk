# go-web-sdk

The Application SDK for web services of Go Minimal, the Standards Lab organization's
minimal-dependency Go standard: the HTTP server and its configuration, routing, RFC 9457
problem responses, the liveness and readiness probes, and middleware. Managed with the marathon
workflow; start from `context/README.md`.

## Design is documented in the landing zone

The design and conventions of this repository are documented in the organization's
[documentation landing zone](https://github.com/standards-lab/docs) — that is the authority.
`context/` records only working knowledge the landing zone and the code do not express; do not
restate documented design here. A change that alters documented behavior updates the landing
zone page in the same effort.

## Repository specifics

- **Module layout** — one Go module rooted at `github.com/standards-lab/go-web-sdk`; the `web`
  package occupies the module root, and `middleware` is its one sub-package. No sub-modules.
- **Dependencies** — the standard library and go-core, per the Go Minimal dependency line.
- **Releases, CI, tests, tasks** — per the Go Minimal standard principles in the landing zone
  (root `v<semver>` tags from `CHANGELOG.md`, hermetic `httptest`/port-0 tests with shared
  helpers in `internal/webtest`, mise tasks).
- **Public repo.** The module resolves through the public Go proxy; CI carries no private-module
  config.
