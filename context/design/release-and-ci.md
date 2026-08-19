# Releases and CI

How the module is versioned, released, built, and tested.

## Releases

The module is one releasable artifact, released by pushing a root tag `v<semver>`; that single
version covers both packages. `.github/workflows/release.yml` cuts a GitHub release with
`taiki-e/create-gh-release-action`, slicing the matching section from `CHANGELOG.md`, which is kept
in Keep-a-Changelog form with dated headings (`## [vX.Y.Z] - YYYY-MM-DD`).

Between releases, a prerelease tag (`v0.2.0-dev.1`) lets a consumer resolve a surface still in
flux. A minor-or-above release purges them: its changelog entry states that the release includes
every `vX.Y.Z-dev.N` change, and those tags are removed.

## Branches and tags

Release preparation commits directly to `main`: the changelog date, the license, and README or
metadata edits. Tagging a commit on a session branch would start the release workflow before `main`
contained that commit. The tag is pushed only after main's CI run passes, so a release never points
at a commit that failed CI.

No release branch is retained. The tag is the durable artifact, and a branch can be recreated from
its tag at any time (`git switch -c release/v0.1.x v0.1.0`). Create a `release/v<major>.<minor>.x`
branch only when a maintenance line exists, meaning a patch to an older minor after `main` has moved
past it, and keep it only as long as that line is supported. Every other branch is deleted when it
merges; the repository has `deleteBranchOnMerge` enabled.

A published tag is not re-cut. Once the module proxy has fetched a version, the checksum database
pins that commit permanently, so moving the tag leaves consumers with a mismatch they cannot
resolve. A failed release workflow does not affect module resolution, because `go get` reads the tag
rather than the GitHub release: fix the workflow on `main` and create the release for the existing
tag with `gh release create`.

## What ships in the module zip

`context/`, `CLAUDE.md`, and `.claude/` are tracked files, so they appear in the module zip
consumers download. Go provides no supported way to exclude them, and they cost nothing: the
toolchain compiles only imported `.go` files. Left as is.

## CI

`.github/workflows/ci.yml` runs `go vet`, `go test -race`, and golangci-lint at the repository
root. The repository is public, so the module resolves through the normal Go proxy and checksum
database; CI carries no `GOPRIVATE` or `.netrc` configuration.

## Tasks

`mise.toml` defines the developer tasks — `build`, `test`, `vet`, `fmt`, `tidy`, `lint` — each a
single-module command. `mise run test` builds and tests the module.
