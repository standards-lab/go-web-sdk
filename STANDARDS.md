# go-web-sdk standards

The judgement calls the standards-reviewer applies to go-web-sdk, beyond what `mise run check` enforces.

- A change that alters documented behavior updates the README and the affected `doc.go` in the same change.
- `architecture/standards/go-elemental/principles/dependencies.md`: the bottom-up line and no provider in a base, across the base module's and each `middleware/` sub-module's `go.mod`.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: the doc.go inventory of `web`, `middleware`, `webtest` and each middleware sub-module, and the harness rules `webtest` realizes.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: the base module with `middleware` and `webtest`, and each capability sub-module under `middleware/`.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the base `v<semver>` and `middleware/<name>/v<semver>` tags, each from its module's own `CHANGELOG.md`, and the mise tasks looping over `GO_MODULES`.
- `architecture/standards/go-elemental/principles/lifecycle-and-context.md`: `Server`'s `Start` and `Shutdown`, the `/readyz` probe over the coordinator's checks, and the construction panics on an unfinalized `Config`, a malformed prefix, a duplicate mount or a group modified after its module is built.
- `architecture/standards/go-elemental/principles/timeouts.md`: `Config`'s server timeouts and `TransferRate`, `Transfer`, and `middleware.Timeout`.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: `ParseQuery`'s `Limits` and `BodyLimit`'s limit, which the caller supplies, and the defaults of `Config` and of `middleware/rate-limit`'s `Config`.
- `architecture/principles/validation-first.md`: `Config.Finalize` and `ParseQuery`.
- `architecture/principles/context-architecture.md`: the README and each `doc.go` are the homes.
