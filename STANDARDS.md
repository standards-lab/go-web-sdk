# go-web-sdk standards

The judgement calls the standards-reviewer applies to go-web-sdk, beyond what `mise run check` enforces.

- A change that alters documented behavior updates the README and the affected `doc.go` in the same change.
- `architecture/standards/go-elemental/principles/dependencies.md`: the base module's and each `middleware/` sub-module's `go.mod`.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: the doc.go inventory of `web`, `middleware`, `webtest`, `upload`, `upload/uploadtest` and each middleware sub-module, and the harness rules `webtest` and `upload/uploadtest` realize.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: the base module with `middleware`, `webtest`, `upload`, and `upload/uploadtest`, and each capability sub-module under `middleware/`.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the base `v<semver>` and `middleware/<name>/v<semver>` tags, each from its module's own `CHANGELOG.md`, and the mise tasks looping over `GO_MODULES`.
- `architecture/standards/go-elemental/principles/lifecycle-and-context.md`: `Server`'s `Start` and `Shutdown`, the `/readyz` probe over the coordinator's checks, and the construction panics on an unfinalized `Config`, a malformed prefix, a duplicate mount or a group modified after its module is built.
- `architecture/standards/go-elemental/principles/timeouts.md`: `Config`'s server timeouts and `TransferRate`, `Transfer`, and `middleware.Timeout`, and `upload.Handler`, which applies its `Transfer` to each request's content.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: `ParseQuery`'s `Limits`, `BodyLimit`'s limit, `upload.Handler`'s `Transfer` limit and `Lifetime`, and `upload.Client`'s `Attempts`, which the caller supplies, and the defaults of `Config` and of `middleware/rate-limit`'s `Config`.
- `architecture/standards/go-elemental/principles/construction-and-execution.md`: `Service`, whose `Run` panics naming a nil field; `middleware.RequestIDOptions`, the one options value `RequestID` takes; `Problem.WriteFor`, the struct-with-method form a problem is written with; `upload.Handler{...}.Routes` and `upload.Client{...}.Create`, struct-with-method forms that panic naming a missing field; and `webtest`'s exported functions and constructors.
- `architecture/principles/comments.md`: every comment.
- `architecture/principles/validation-first.md`: `Config.Finalize` and `ParseQuery`.
- `architecture/principles/context-architecture.md`: the README, each `doc.go`, and the pages under `docs/`, which hold the reasoning behind the unexported machinery.
