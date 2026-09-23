# Error handling: deferred extension points

Two extension points of the error-returning handler stay unbuilt until a consumer asks for one.

## Writer inheritance

`Group.HandleErr` requires an `ErrorWriter` on its own group, so a child group mounted under a
parent that has one still panics. If a layer wants one writer at `/api` for every domain group,
resolution moves to `NewModule`, which walks the tree. The panic stays at wiring time.

## Built-in errors before the matchers

`ErrorWriter` maps its own error types before it consults the consumer's matchers, so a consumer
cannot give `*QueryError` or the other two built-in errors a problem type of its own. The most
common problem a paginated API emits stays `about:blank`. Reversing the order is a breaking
change, and `TestErrorWriter_BuiltInWinsOverMatchers` asserts the current order.

## Promotion candidates

The reference service's `sdk` package stages two candidates for `web`: `PathID`, a typed
path-value parse returning a `PathError`, and `Command`, the guarded-command read that composes
`PathID` with `IfMatch` and `DecodeJSON`.
