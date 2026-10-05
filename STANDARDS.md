# go-web-sdk standards

The judgement calls the standards-reviewer applies to go-web-sdk.

- A change that alters documented behavior updates the README and the affected `doc.go` in the same change.
- `architecture/standards/go-elemental/principles/dependencies.md`: the bottom-up line and no provider in a base, across the base `go.mod` (go-core alone) and each middleware sub-module's `go.mod`, which admits one sourced library under a category its README names.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: the doc.go inventory of `web`, `middleware`, `webtest` and each middleware sub-module, and the harness rules `webtest` realizes.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: the base module with `middleware` and `webtest`, and each middleware that needs a third-party library as a sub-module under `middleware/`, which the base module never imports.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the base `v<semver>` and `middleware/<name>/v<semver>` tags, each from its module's own `CHANGELOG.md`, and the mise tasks looping over `GO_MODULES`.
- `architecture/standards/go-elemental/principles/timeouts.md`: `Config`'s tight server timeouts and `TransferRate`, `Transfer`'s per-route connection deadlines, and `middleware.Timeout`, which never sits over a route shorter than the transfer it allows.
- `architecture/principles/context-architecture.md`: the README and each `doc.go` are the homes; `context/` records only what they do not express.
