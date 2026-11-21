# Problems and errors

This page holds the reasoning behind [`Problem`](../problem.go) and the
[`ErrorWriter`](../errorwriter.go).

## Status policy stays with the matchers

The package's own errors each carry their status through an interface whose method is
unexported, so a consumer cannot opt its own errors into it: a consumer's status policy stays in
its `ProblemMatcher` list.

A handler that returns a `Problem` whole has decided the response itself; `Write` sends it
without adding the error's text.

The error writer's records name their attributes as `middleware.RequestLogger` names its own,
so the two read as one log.

## Decoding a problem

`Problem` marshals its five standard members through a separate struct with the same json tags,
so marshaling them never re-enters `Problem`'s own `MarshalJSON`. On decoding, the five standard
members are stripped from `Extras`, matched case-insensitively because `encoding/json`
matches the struct's fields case-insensitively. Otherwise a non-conforming document (a
capitalized `"Status"`) would both populate the typed field and leak the same member into
`Extras`.
