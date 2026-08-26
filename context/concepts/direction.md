# Candidate direction

What the HTTP layer still needs, migrated from the predecessor repository's planning. Everything
here is unbuilt and deferred deliberately — each item waits on a consumer that would validate its
API, a build session settles that API in plan mode before anything lands, and the roadmap re-plan
decides what is next. None of it revises the current API; all of it adds to it.

## The rest of the middleware set

New implementations land in the `middleware` package: authentication and authorization
enforcement wait on an auth infrastructure library (`concepts/service-middleware.md` records the
open placement question), CORS waits on a browser client, and a recovery handler and a request
ID wait for a service to need them.

## Error mapping

The domain-error-to-status matchers that turn a returned error into a problem response. Additive
to the problem writers; needs a domain handler to exercise it.

## A readiness type hook

`/readyz` attaches its `checks` extension member to an `about:blank` problem (the landing zone's
[problem responses page](https://github.com/standards-lab/docs/blob/main/standards/go-minimal/go-web-sdk/problems.md)).
A consumer that needs readiness failures under its own problem vocabulary gets a type hook on
`Readiness`, not an SDK-owned URI.
