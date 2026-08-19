# Problem responses

The design of the RFC 9457 problem writers. The code and `doc.go` are authoritative for the API;
this note records the reasoning.

## The SDK defines no problem types

RFC 9457's `type` member is the problem's identity — the member a client branches on, with `title`
advisory and `status` an advisory copy of the status line. A type URI therefore names an
*application's* vocabulary, and an SDK that mints one claims semantics it does not own. So every
problem the SDK emits is `about:blank`, which the RFC defines as "no semantics beyond the HTTP
status code", and a consumer brings its own URIs through `Problem.Write` or the extras map of
`WriteProblemWith`.

## The `checks` extension member

`/readyz` attaches a `checks` extension member (RFC 9457's term) to an `about:blank` problem, and
the RFC means extension members to be defined by the problem type. The trade-off is accepted: if a
consumer needs readiness failures under its own vocabulary, the answer is a type hook on
`Readiness`, not an SDK-owned URI — deferred until something asks for it
(`concepts/direction.md`).

## Defaults keep the document consistent with the status line

A zero `Status` defaults to 500. An empty title defaults to the status phrase — what the RFC asks
of an `about:blank` problem, and what keeps a hand-typed title from drifting away from the status
code it accompanies — and is omitted for a code outside the standard table rather than invented.
Extras may add or override any member except `status`, which is re-seeded after the copy so the
body always matches the status line.
