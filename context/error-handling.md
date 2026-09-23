# Error handling: deferred extension points

Two extension points of the error-returning handler stay unbuilt until a consumer asks for one.
The reference service also stages two helpers as candidates for the `web` package.

## Writer inheritance

`Group.HandleErr` requires an `ErrorWriter` on its own group, so a child group mounted under a
parent that has one still panics. When a consumer wants one writer at `/api` for every domain
group, `NewModule`, which walks the tree, takes over resolving the writer. The panic for a missing
writer then moves from registration to `NewModule`, still at wiring time.

## Built-in errors before the matchers

`ErrorWriter` maps its own error types before it consults the consumer's matchers, so a consumer
cannot give `*QueryError` or the other two built-in errors a problem type of its own. The most
common problem a paginated API emits stays `about:blank`. Reversing the order is a breaking
change, and `TestErrorWriter_BuiltInWinsOverMatchers` asserts the current order.

## Candidates staged in the reference service

The reference service's `sdk` package stages two candidates for promotion into `web`: `PathID`,
which parses a typed path value and returns a `PathError`, and `Command`, the guarded-command read
that composes `PathID` with `IfMatch` and `DecodeJSON`.
