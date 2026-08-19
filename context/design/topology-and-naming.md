# Topology and naming

How the repository is organized and named.

## One module, one cohesive package

The repository is a single Go module rooted at `github.com/standards-lab/go-web-sdk`; the `web`
package occupies the module root. `web` is one cohesive package rather than a set of concern
packages (routing, health, problems as packages of their own), because the SDK's elements are
designed to work together: a concern split would reintroduce the name duplication a flat package
avoids (`problem.Problem`, `middleware.Middleware`) and scatter types that reference each other, for
a naming gain alone. The cost is that names take the prefix a sub-package would have supplied —
`WriteProblem` rather than `problem.Write` — which is ordinary Go.

A sub-package is earned by growth or by dependency weight, never by topic. `middleware` is the one
sub-package: the middleware implementations are the SDK's growth area — CORS, recovery, a request ID
are all foreseeable — so they get a package to grow in without enlarging `web`, while the
`Middleware` type and `Chain` stay in `web`, whose routing layer consumes them. The import runs
`middleware → web` only. A dependency heavy enough that the rest of the SDK should not compile it
would have to leave the module entirely; no such dependency exists, and the module has no
sub-modules — an application SDK has no providers.

`internal/webtest` is module-private test infrastructure, invisible to consumers; see
`design/tests-and-docs.md`.

## Naming

- **Repository / module:** `github.com/standards-lab/go-web-sdk` under the `standards-lab`
  organization. The `-sdk` suffix marks a development kit.
- **Packages** keep their own short names — `web` at the module root, `middleware` beneath it — so
  the import path names the tier and the identifier in code stays the package's: `web.Server`,
  `middleware.RequestLogger`.
- **Release tags:** the module is tagged `v<semver>` at the repository root.
