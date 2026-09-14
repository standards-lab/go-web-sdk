# The adapter's remaining work and the whole-response problem story

Settled at the 2026-08-31 workspace retrospective; `v1.web.adapter` in the coordinator's
roadmap cites this note. The adapter core landed in go-web-sdk v0.6.0 under
`v1.data.sql.integration.websdk` (2026-09-05): `HandlerFunc`, `Handle`, `Group.SetErrorWriter`,
`Group.HandleErr`, and the request helpers `IfMatch` and `DecodeJSON`. The committed-tracking
`Recorder` in `recorder.go` and the `ErrorWriter` problem vocabulary (`ProblemMatcher`,
`Problem.Extras`, the readiness type hook) joined them since. The code and `doc.go` express
those, and this note keeps only what remains. It decays into the code and the package
documentation when the adapter task lands.

## Idiom

Wiring-time methods are the SDK's form for optional behavior: `Group.Use`,
`Group.SetErrorWriter`, `ErrorWriter.Detail`, `ErrorWriter.Log`. Struct options are for
construction-time configuration accumulated from sources, go-core's `config.Options` being the
case. The go-patterns constructors reference draws the same line: struct configs for production
constructors, variadic or method-style variation where the parameters are genuinely optional. An
earlier version of this note stated the rule as "never functional options", which overstated it.

## What remains for `v1.web.adapter`

1. **Router-level misses join the RFC 9457 contract.** Unmatched paths and method mismatches
   fall through to `http.ServeMux` as `text/plain` 404/405 — bare text on an API whose whole
   error story is problem+json, with the 405's `Allow` header outside the SDK's control. Add
   NotFound/MethodNotAllowed hooks on the router (and module) so misses write problems.
2. **`http.Server` escape hatches, and the swallowed encoder error.** `NewServer` sets `Addr`,
   `Handler`, and four timeouts and nothing else. `ErrorLog` is nil, so TLS handshake failures,
   parse errors, and the superfluous-WriteHeader warning go to global `log` on stderr,
   invisible to the slog pipeline — bridge it. `MaxHeaderBytes` is unset (the body-limit
   middleware bounds bodies, not headers) — a `web.Config` field. `Handle` discards the
   encoder's error from `ErrorWriter.Write` with a blank assignment, as every handler does with
   `WriteJSON`; whether an encoder failure deserves a log line through `ErrorWriter.Log` is
   decided here.
3. **Writer inheritance.** `HandleErr` requires the writer on its own group; a child mounted
   under a parent that has one still panics. If a layer wants one writer at `/api` for every
   domain group, resolution moves to `NewModule`, which walks the tree — the registration-time
   panic becomes a compile-time one, still at wiring. Wait for a consumer to ask.
4. **`statusError` precedence over the matchers.** The built-in check still runs before
   `ErrorWriter`'s matchers, so a consumer cannot give `*QueryError` (or the SDK's other two
   built-in errors) its own problem type — the most common problem a paginated API emits stays
   `about:blank`. Left alone when the problem vocabulary landed: reversing the precedence would
   be another independently-breaking change on top of what that step already shipped, and
   `errorwriter_test.go`'s `TestErrorWriter_BuiltInWinsOverMatchers` asserts the current order
   deliberately. Wait for a consumer to ask.
5. **The config env segment becomes per-block.** `config.go` hardcodes the `"server"` segment
   in every composed name (`APP_SERVER_PORT` unconditionally), so a second `web.Config` block —
   the management listener (`v1.admin-listener`) needs — cannot exist under one prefix. Give
   the env composition a block-name parameter. Sequenced with the listener but owned by this
   SDK; land it wherever an earlier session touches config.

## Promotion candidates staged in the reference service

The reference service's `sdk` package stages two candidates for this SDK: `PathID`, a typed
path-value parse returning a `PathError`, and `Command`, the guarded-command read composing
`PathID` with `IfMatch` and `DecodeJSON`.
