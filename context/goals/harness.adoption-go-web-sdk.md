# goal · harness.adoption-go-web-sdk

- **State:** building
- **Task:** adopt
- **Branch:** adopt

## Tasks

1. [ ] adopt

## Task brief · adopt

**Problem**

go-web-sdk and go-messaging don't pin marathon, and their checks don't run lint-context or
lint-references. Both fail lint-context: neither capability map is marked, and go-messaging's
README is 684 words over its 1,500 budget. About 1,460 lines of Go comments and about 200 lines of
configuration and script comments fall outside the five kinds. go-messaging trails on mise-action
and PostgreSQL, and go-web-sdk's rate-limit module trails its base module.

**Behaviors**
1. After each repository's upgrade slice, its currency command reports nothing trailing:
   rate-limit requires go-web-sdk v0.18.0, with a "Changed" line under rate-limit's Unreleased;
   go-messaging's CI runs mise-action v5.2.10, and its PostgreSQL stack runs 19.0-alpine with the
   integration suite passing.
2. Each repository pins marathon 0.22.0 as `github:standards-lab/claude-plugins` with prefix
   `marathon/v`. `mise install` puts both lints on PATH, and currency reports a newer marathon tag.
3. Each check also runs lint-context and lint-references from the repository root and fails when
   either fails. A lint missing from PATH fails the check with a message naming `mise install`.
   The check's description names everything it runs. CI passes with the tools mise-action
   installs.
4. lint-context passes in both repositories:
   - Each map entry is marked `(built)` or `(planned)`, with no prose under the heading.
   - go-web-sdk's error-handling and middleware-sourcing notes become linked `(planned)` entries.
   - go-messaging's Usage detail moves unchanged to a docs page the README links. The README keeps
     its overview, Modules, Packages, Development, and Releases sections, and every pointer to a
     moved section names the new page.
5. Every comment in Go source and tests is one of the five kinds. Unexported doc comments,
   test-function doc comments, and in-body comments go; their reasoning moves into names, subtest
   names, exported docs, or docs/. Provenance lines stay. SQL file headers stay as package
   documentation (in sqlate's format, also template metadata).
6. Every comment in configuration and scripts is one of the five kinds: mise.toml, golangci,
   sqlint, workflows, Dockerfiles, compose, .gitignore, and marathon.toml. Each script's section
   comments become named functions. Reasoning moves into task descriptions, names, or one-line
   references that lint-references resolves.
7. Every module's golangci configuration enables nolintlint with require-explanation and
   require-specific.
8. Each STANDARDS.md gains a comment rule citing comments.md for source, tests, scripts, and every
   configuration file, and saying a data file's header is its package documentation (a sqlate
   SQL file's header is also its template metadata).
9. go-web-sdk's backlog mention names `standards-lab/roadmap/index.toml`.
10. Both repositories' CI passes, go-messaging's integration job included. CHANGELOG entries record
    only `go.mod` requirement changes.

**Test seams**
- Each repository's `mise run check` and `mise run currency`; go-messaging's `mise run integration`.

**Slices**
1. go-web-sdk upgrade: rate-limit's requirement.
2. go-messaging upgrade: mise-action and PostgreSQL 19.
3. go-web-sdk comments, nolintlint, and comment rule.
4. go-web-sdk context, pin, and check.
5. go-messaging comments, nolintlint, and comment rule.
6. go-messaging README Usage to docs/, context, pin, and check.

**Out of scope**

Releases or tags, a Markdown link check, budgets for docs/, rewording exported docs, feature work.

**Door**

Two-way. Nothing is tagged, and every change stays under Unreleased.

## Progress

slices 6/6 committed · standards ✓ · spec ✓ · editor ✓

## Decisions

None.

## Pending edits

- coordinator · context/messaging.md:6 and docs/messaging.md:5: say "the go-messaging README and
  its docs/" where they name the README as the home of its behavior.
