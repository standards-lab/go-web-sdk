# go-web-sdk

The application SDK for web services of `go-minimal`, the Standards Lab organization's
minimal-dependency Go standard: the HTTP server and its configuration, routing, RFC 9457 problem
responses, the liveness and readiness probes, and middleware. Managed with the marathon workflow;
start from `context/README.md`.

## Conventions are settled in the repository

The design and conventions for this SDK are recorded in `context/design/` — that is the authority.
Keep them there; do not restate them here.

## Role boundary

go-web-sdk is a marathon **code** project (`.claude/marathon.toml` declares `kind = "code"`). The
developer owns the production Go source — they apply it and answer for it. The agent writes
everything else: tests, godoc and `doc.go`, prose documentation, the files in `context/`, the
implementation guide, and the reset file.

## Repository specifics

- **Module layout** — one Go module rooted at `github.com/standards-lab/go-web-sdk`; the `web`
  package occupies the module root, and `middleware` is its one sub-package. No sub-modules.
- **Dependencies** — the standard library and `go-core`; at most, packages as idiomatic and stable
  as the standard library. Vendor SDKs never enter this module.
- **Releases** — the module is tagged `v<semver>` at the root from `CHANGELOG.md`, cut by
  `.github/workflows/release.yml`.
- **Tests** are co-located `{file}_test.go` files in an external black-box package
  (`package <pkg>_test`) that exercise the public API. They are hermetic: `httptest` recorders and
  ephemeral ports, never a fixed port. Helpers more than one test package needs live in
  `internal/webtest`.
- **Tasks** run through `mise` (`build`, `test`, `vet`, `fmt`, `tidy`, `lint`).
- **Public repo.** The module resolves through the public Go proxy; CI carries no private-module
  config.
