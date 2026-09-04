# The error-handler adapter and the whole-response problem story

Settled at the 2026-08-31 workspace retrospective; `v1.web.adapter` in the coordinator's
roadmap cites this note. §1 is the settled adapter decision from the retrospective's design
discussion; §2 is the retrospective's evaluation findings folded into the same session because
the adapter multiplies traffic through the surfaces they fix. This note decays into the
landing-zone pages and the code when the session lands.

## 1. The adapter

### Decision

Add an opt-in adapter that lets a handler return an error, routed through an `ErrorWriter`.
The stdlib `http.Handler` signature stays the primary contract: `Group.Handle` and
`Router.Handle` continue to accept `http.Handler`, and nothing in the SDK requires the
adapter.

### Why the original choice was a mistake

The SDK kept the stdlib handler signature to avoid a framework-style handler type. That goal
is still right, but it was applied one layer too deep. The cost shows in the reference
service: `domain/organization/handler.go` repeats

```go
if err != nil {
    _ = h.errors.Write(w, r, err)
    return
}
```

fourteen times across seven handlers, and two helpers (`pathID`, `decode`) take `w` only so
they can write their own rejection. Every one of those sites is a chance to forget the
`return`, write twice, or map a status inconsistently. The error-returning handler is the one
framework idea that removes real defects rather than adding abstraction, and it composes with
`net/http` rather than replacing it.

### API

```go
// HandlerFunc is an http.HandlerFunc that reports failure by returning an
// error instead of writing it. A nil return means the handler wrote the
// response itself.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts fn into an http.Handler: a returned error is written as a
// problem response through ew. A handler that has already written to w and
// then returns an error produces a logged, not written, failure — the
// adapter never writes a second response.
func Handle(fn HandlerFunc, ew *ErrorWriter) http.Handler
```

Convenience on `Group`, so the adapter reads naturally at the registration site:

```go
// HandleErr registers an error-returning handler under the group's error
// writer. Use after NewModule panics.
func (g *Group) HandleErr(method, pattern string, fn HandlerFunc, mw ...Middleware)
```

`Group` gains one field, the `*ErrorWriter` its `HandleErr` routes use; a group with no writer
and a `HandleErr` call panics at wiring time, consistent with the SDK's registration-time
failure rule. The writer is group-scoped, not per-route: the reference service builds one
writer per layer today and would otherwise re-thread it through seven call sites. How the
writer is set follows the workspace's options idiom — a struct-shaped options value in
go-core's style (`config.Options` with zero-value defaults), never functional options, which
nothing in the workspace uses; the setter must respect `checkSeal()`.

### Effect on the reference service

```go
func (h *handler) find(w http.ResponseWriter, r *http.Request) error {
    id, err := pathID(r)
    if err != nil {
        return err
    }
    o, err := h.service.Find(r.Context(), id)
    if err != nil {
        return err
    }
    return web.WriteJSON(w, http.StatusOK, o)
}
```

`pathID` becomes `func(*http.Request) (string, error)`; `decode` becomes
`func[T any](*http.Request) (T, error)` with `MaxBytesReader` moved to a per-route middleware
or kept by passing `w` for the reader only. `Routes` registers through `g.HandleErr` and drops
the `errors` field from `handler`, since the group owns the writer.

### Double-write rule

The adapter must not write a second response. The chosen mechanism: wrap `w` in a shared
status-recording writer, and if a status was already written when the error returns, log at
error level and write nothing. (The documented alternative — rely on `net/http`'s
superfluous-`WriteHeader` log line — was rejected as weaker; it also emits to stderr, not
slog, see §2.4.) The shared writer is the same type `RequestLogger`, the adapter, and the
future recoverer use.

## 2. Folded retrospective findings

Verified against v0.5.0 (96.5% / 89.7% coverage, all hermetic; the findings are headroom, not
rot).

1. **The shared wrapped writer must be built, not reused, and it lives in `web`.**
   `middleware/logger.go`'s `statusRecorder` overrides `WriteHeader` and `ReadFrom` but not
   `Write`, so a handler that writes a body then calls `WriteHeader(500)` records 500 while
   the client got 200 — and the landing-zone middleware page's claim that seeding 200 "removes
   any need to intercept the body write" is wrong and must be corrected in the same effort.
   The adapter needs a *committed* flag more than a status. Because `middleware` imports `web`
   and never the reverse, the shared type moves into `web` and the logger is rewritten onto
   it.
2. **`ErrorWriter` gains a problem vocabulary.** `StatusMatcher` is `func(error) (int, bool)`
   and `Write` hardcodes empty type and title, so every problem the service emits is
   `about:blank`, distinguishable only by status — while the landing zone's problems page
   promises consumers their own URIs through extension points that do not exist on this path.
   Widen the matcher (or add a problem-returning matcher alongside) so a matcher can carry a
   type URI, title, and extension members. Do this in the adapter session, before `HandleErr`
   multiplies the call sites. Related: `WriteProblemWith` rebuilds the document as a
   `map[string]any` while `Problem.Write` marshals the struct — two serializers for one
   document; unify.
3. **Router-level misses join the RFC 9457 contract.** Unmatched paths and method mismatches
   fall through to `http.ServeMux` as `text/plain` 404/405 — bare text on an API whose whole
   error story is problem+json, with the 405's `Allow` header outside the SDK's control. Add
   NotFound/MethodNotAllowed hooks on the router (and module) so misses write problems.
4. **`http.Server` escape hatches.** `NewServer` sets `Addr`, `Handler`, and four timeouts and
   nothing else. `ErrorLog` is nil, so TLS handshake failures, parse errors, and the
   superfluous-WriteHeader warning (the adapter's own diagnostic, per §1) go to global `log`
   on stderr, invisible to the slog pipeline — bridge it. `MaxHeaderBytes` is unset (the
   body-limit middleware bounds bodies, not headers) — a `web.Config` field.
5. **The request-side helpers promote here.** The service's `sdk/web.go` stages `IfMatch` +
   `PreconditionError` for this SDK (their `doc.go` says so); `decode[T]`
   (MaxBytesReader + DisallowUnknownFields) is its unstaged twin, and it currently collapses a
   body-overflow into the validation error so an oversized body answers 400 where 413 is
   correct — fix on promotion. The SDK today has no request-side body or conditional-header
   API at all; these are its first, and they are the natural first error-returning helpers, so
   they land with the adapter.
6. **The config env segment becomes per-block.** `env.go` hardcodes the `"server"` segment in
   every composed name (`APP_SERVER_PORT` unconditionally), so a second `web.Config` block —
   the management listener `v1.data.sql.integration.listener` needs — cannot exist under one
   prefix. Give the env composition a block-name parameter. Scheduled with the listener task
   but owned by this SDK; land it wherever the earlier session touches config.
