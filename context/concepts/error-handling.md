# The adapter's remaining work and the whole-response problem story

Settled at the 2026-08-31 workspace retrospective; `v1.web.adapter` in the coordinator's
roadmap cites this note. The adapter core landed in go-web-sdk v0.6.0 under
`v1.data.sql.integration.websdk` (2026-09-05): `HandlerFunc`, `Handle`, `Group.SetErrorWriter`,
`Group.HandleErr`, the committed-tracking `recorder` in `handler.go`, and the request helpers
`IfMatch` and `DecodeJSON`. The code and `doc.go` express those, and this note keeps only what
remains. It decays into the landing-zone pages and the code when the adapter task lands.

## Idiom

Wiring-time methods are the SDK's form for optional behavior: `Group.Use`,
`Group.SetErrorWriter`, `ErrorWriter.Detail`, `ErrorWriter.Log`. Struct options are for
construction-time configuration accumulated from sources, go-core's `config.Options` being the
case. The go-patterns constructors reference draws the same line: struct configs for production
constructors, variadic or method-style variation where the parameters are genuinely optional. An
earlier version of this note stated the rule as "never functional options", which overstated it.

## What remains for `v1.web.adapter`

Verified against v0.5.0 at the retrospective (96.5% / 89.7% coverage, all hermetic); re-read
against v0.6.0 at this rewrite.

1. **The recorder is exported and the logger rewritten onto it.** `handler.go`'s `recorder`
   tracks the commit through `WriteHeader`, `Write`, and `ReadFrom` — the case
   `middleware/logger.go`'s `statusRecorder` misses: it overrides `WriteHeader` and `ReadFrom`
   but not `Write`, so a handler that writes a body then calls `WriteHeader(500)` records 500
   while the client got 200. Export the recorder, rewrite the logger onto it, and correct the
   landing-zone middleware page's claim that seeding 200 "removes any need to intercept the
   body write" in the same effort. `middleware` imports `web` and never the reverse, so the
   type stays in `web`.
2. **`ErrorWriter` gains a problem vocabulary.** `StatusMatcher` is `func(error) (int, bool)`
   and `Write` sends an empty type and title, so every problem the service emits is
   `about:blank`, distinguishable only by status — while the landing zone's problems page
   promises consumers their own URIs through extension points that do not exist on this path.
   Widen the matcher (or add a problem-returning matcher alongside) so a matcher can carry a
   type URI, title, and extension members. The SDK's own errors map themselves through the
   unexported `statusError` interface in `errors.go`, sealed on purpose because consumer policy
   is the matcher list; whether that seam is exported so an error can carry its problem is this
   item's decision. Related: `WriteProblemWith` rebuilds the document as a `map[string]any`
   while `Problem.Write` marshals the struct — two serializers for one document; unify. Also
   related: `/readyz` attaches its `checks` extension member to an `about:blank` problem, and a
   consumer that needs readiness failures under its own vocabulary gets a type hook on
   `Readiness`, not an SDK-owned URI.
3. **Router-level misses join the RFC 9457 contract.** Unmatched paths and method mismatches
   fall through to `http.ServeMux` as `text/plain` 404/405 — bare text on an API whose whole
   error story is problem+json, with the 405's `Allow` header outside the SDK's control. Add
   NotFound/MethodNotAllowed hooks on the router (and module) so misses write problems.
4. **`http.Server` escape hatches, and the swallowed encoder error.** `NewServer` sets `Addr`,
   `Handler`, and four timeouts and nothing else. `ErrorLog` is nil, so TLS handshake failures,
   parse errors, and the superfluous-WriteHeader warning go to global `log` on stderr,
   invisible to the slog pipeline — bridge it. `MaxHeaderBytes` is unset (the body-limit
   middleware bounds bodies, not headers) — a `web.Config` field. `Handle` discards the
   encoder's error from `ErrorWriter.Write` with a blank assignment, as every handler does with
   `WriteJSON`; whether an encoder failure deserves a log line through `ErrorWriter.Log` is
   decided here.
5. **Writer inheritance.** `HandleErr` requires the writer on its own group; a child mounted
   under a parent that has one still panics. If a layer wants one writer at `/api` for every
   domain group, resolution moves to `NewModule`, which walks the tree — the registration-time
   panic becomes a compile-time one, still at wiring. Wait for a consumer to ask.
6. **The config env segment becomes per-block.** `config.go` hardcodes the `"server"` segment
   in every composed name (`APP_SERVER_PORT` unconditionally), so a second `web.Config` block —
   the management listener (`v1.admin-listener`) needs — cannot exist under one prefix. Give
   the env composition a block-name parameter. Sequenced with the listener but owned by this
   SDK; land it wherever an earlier session touches config.

## Promotion candidates staged in the reference service

The reference service's `sdk` package stages two candidates for this SDK: `PathID`, a typed
path-value parse returning a `PathError`, and `Command`, the guarded-command read composing
`PathID` with `IfMatch` and `DecodeJSON`.
