# Candidate direction

What the HTTP layer still needs, migrated from the predecessor repository's planning. Everything
here is unbuilt and deferred deliberately — each item waits on a consumer that would validate its
API, a build session settles that API in plan mode before anything lands, and the roadmap re-plan
decides what is next. None of it revises the current API; all of it adds to it.

## The rest of the middleware set

Superseded (2026-08-31) by the retrospective's settled direction: `v1.web.adapter` brings the
error-handler adapter and the shared wrapped writer, and `v1.web.middleware` builds the
hand-rolled set (request ID, recoverer, timeout, content-type gate, body limit, fixed headers)
and sources the spec-surface set per the org's dependency-sourcing rule (`standards-lab
context/design/dependency-sourcing.md`) — the reference service now needs them.
Authentication and authorization enforcement land in the auth infrastructure library over
stdlib types (`concepts/service-middleware.md`, settled); transport-generic implementations
land in the `middleware` package.

## A readiness type hook

`/readyz` attaches its `checks` extension member to an `about:blank` problem (the landing zone's
[problem responses page](https://github.com/standards-lab/docs/blob/main/standards/go-elemental/go-web-sdk/problems.md)).
A consumer that needs readiness failures under its own problem vocabulary gets a type hook on
`Readiness`, not an SDK-owned URI.
