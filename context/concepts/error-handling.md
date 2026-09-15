# The adapter: deferred extension points

Two items from the 2026-08-31 workspace retrospective's list stay deliberately unbuilt, each
waiting on a consumer that has not yet asked. The rest of that list — router 404/405 hooks, the
`ErrorLog` bridge, `MaxHeaderBytes`, the swallowed encoder error, and the per-block config env
segment — landed at `v1.web.tasks.adapter`'s close (2026-09-14) and now lives in the code and
`doc.go`.

## Idiom

Wiring-time methods are the SDK's form for optional behavior: `Group.Use`,
`Group.SetErrorWriter`, `ErrorWriter.Detail`, `ErrorWriter.Log`, `Server.Log`,
`Router.SetNotFound`. Struct options are for construction-time configuration accumulated from
sources, go-core's `config.Options` being the case. The go-patterns constructors reference draws
the same line: struct configs for production constructors, variadic or method-style variation
where the parameters are genuinely optional.

## Deferred

1. **Writer inheritance.** `HandleErr` requires the writer on its own group; a child mounted
   under a parent that has one still panics. If a layer wants one writer at `/api` for every
   domain group, resolution moves to `NewModule`, which walks the tree — the registration-time
   panic becomes a compile-time one, still at wiring. Wait for a consumer to ask.
2. **`statusError` precedence over the matchers.** The built-in check still runs before
   `ErrorWriter`'s matchers, so a consumer cannot give `*QueryError` (or the SDK's other two
   built-in errors) its own problem type — the most common problem a paginated API emits stays
   `about:blank`. Left alone when the problem vocabulary landed: reversing the precedence would
   be another independently-breaking change on top of what that step already shipped, and
   `errorwriter_test.go`'s `TestErrorWriter_BuiltInWinsOverMatchers` asserts the current order
   deliberately. Wait for a consumer to ask.

## Promotion candidates staged in the reference service

The reference service's `sdk` package stages two candidates for this SDK: `PathID`, a typed
path-value parse returning a `PathError`, and `Command`, the guarded-command read composing
`PathID` with `IfMatch` and `DecodeJSON`.
