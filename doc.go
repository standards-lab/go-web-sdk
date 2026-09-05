// Package web provides the HTTP layer: a net/http server bound to a
// caller-supplied handler, RFC 9457 problem responses, a JSON writer, and the
// liveness and readiness endpoints an orchestrator probes.
//
// # Server
//
// [NewServer] wraps an http.Server built from a finalized [Config] and a
// handler the caller composes; an unfinalized Config panics with the fix
// named. [Server.Start] binds the listener on the calling goroutine — the
// context bounding the bind — and only then serves in the background, so a
// bind failure is returned to the caller instead of being lost in a
// goroutine. [Server.Addr] reports the bound address once started; a
// configured port 0 binds an ephemeral port, and Addr reads back the
// assignment.
//
// [Server.Err] belongs to a successful serve session: after Start returns nil,
// a serve failure arrives on it, and the channel closes when serving stops.
// http.ErrServerClosed is the expected end of a shutdown and is not reported.
// [Server.Shutdown] before a successful Start is a no-op that leaves the
// server startable, so a lifecycle drain after a failed startup passes
// through cleanly; once it has served, a Server is single-use — construct a
// new one to serve again.
//
// # Lifecycle wiring
//
// The package registers no lifecycle service of its own and holds no shutdown
// timeout. [Server.Start] and [Server.Shutdown] match the member signatures of
// go-core's [lifecycle.Service], and [Server.Err] is a monitorable source, so
// a composition root declares the server as bare method values — in the root
// stage, so every numbered stage starts beneath it and the drain empties it
// first:
//
//	lc.Add(lifecycle.Service{
//		Name:     "server",
//		Stage:    lifecycle.StageRoot,
//		Start:    srv.Start,
//		Shutdown: srv.Shutdown,
//	})
//	lc.Monitor(srv.Err())
//
// A bind failure fails the coordinator's startup, a serve failure ends its
// run, and Shutdown receives the timeout-bounded drain context
// [Server.Shutdown] consumes directly.
//
// # Routing
//
// [Group], [Module], and [Router] compose an application's route tree. A
// Group declares routes: a path prefix (multi-segment prefixes such as
// "/api/v1" are first-class), a middleware stack, atomic routes, and nested
// child groups. [NewModule] compiles a group tree once into a [Module] —
// every route under its full pattern with its middleware baked in, group
// stacks outermost ordered root to leaf, then per-route middleware — and
// seals the tree, so a route registered after compilation panics instead of
// going silently dead. [NewHandlerModule] mounts a raw handler under a prefix
// (an embedded client application, a file server), the one case where a
// module strips the prefix from the request.
//
// A [Router] dispatches to mounted modules by longest-prefix match on segment
// boundaries, falling back to a native http.ServeMux for every path no module
// owns. [Router.Handle] mirrors ServeMux.Handle on the native mux — *Router
// satisfies [Mounter], so [RegisterHealth] mounts the probes there,
// structurally outside every module's middleware — and [Router.Use] wraps the
// whole dispatch. The effective order is router middleware, then group
// middleware root to leaf, then route middleware, then the handler.
// Registration mistakes — a malformed prefix, a duplicate pattern, a second
// module at a prefix, a sealed-group mutation — panic at wiring time; nothing
// recomposes or validates per request.
//
// # Configuration
//
// [Config] holds the host, the port, and the server's four timeouts, and
// implements the Merge and Finalize contract of go-core's config package, so
// it loads as part of an application's configuration rather than on its own. The port and
// timeouts are pointers: nil is unset and takes the default, while an explicit
// zero survives the load and means what it says — a disabled timeout, or an
// ephemeral port. A file and the environment express both states identically.
// Finalize composes its environment override names from the prefix it
// receives (via [NewEnv], recorded on [Env] for introspection), applies
// defaults, reads the overrides, and validates; an empty prefix disables the
// overrides.
//
// # Health
//
// [Liveness] reports that the process is up and serving HTTP and checks nothing
// else; an unanswered probe is the liveness signal. [Readiness] aggregates the
// [lifecycle.Check] values the caller supplies, and answers 503 unless every
// one of them is ready. A Check with a nil Checker reports not ready, so a
// subsystem that failed to construct fails the probe. [RegisterHealth] mounts
// both endpoints on a [Mounter]: liveness plain, and readiness over a
// [lifecycle.Coordinator], queried fresh on every request rather than once at
// registration, so a service the coordinator gains after RegisterHealth is
// called still appears on the next probe. The coordinator itself is the first
// participant, under the fixed name "lifecycle", followed by its own Checks,
// in start order. Exposing an in-progress service's check this way is safe
// only because the transport registers at [lifecycle.StageRoot]; nothing else
// guarantees every check exists before a request reaches the probe. The
// patterns use net/http.ServeMux's method-scoped syntax ("GET /healthz"),
// which http.ServeMux handles directly; any other Mounter translates them.
//
// # Middleware
//
// A [Middleware] wraps one http.Handler in another, and [Chain] composes a set
// of them around a handler in argument order: in Chain(h, a, b), a sees the
// request first. A nil entry is skipped, so a caller can build a chain with
// conditional entries without filtering it first.
//
// The type and the composer live here because the routing layer consumes
// them; the middleware implementations — the request logger today — live in
// the middleware package.
//
// # Paginated reads
//
// [ParseQuery] parses a read request's query string in full into a [Query]:
// the page, size, and sort parameters, and every remaining parameter as the
// filter set — one call yields both halves, so a handler cannot parse the
// paging parameters and forget to strip them from the filters. Sort is
// comma-separated field names, "-" prefixing a descending key
// ("sort=name,-code"), honored across every occurrence of the parameter;
// sort and filter names are lexical here, and whether one names a readable
// field is the data layer's check. Policy belongs to the caller: a [Limits]
// value supplies the default and maximum size (invalid limits panic as a
// wiring mistake), and a malformed or out-of-bounds parameter returns a
// *[QueryError]. On success, [NewPage] assembles the [Page] envelope —
// items, page, size, total, with nil items marshaling as [] — and
// [WriteJSON] sends it as the response body.
//
// # Request helpers
//
// [IfMatch] reads a request's version precondition from the If-Match header
// (RFC 9110 §13.1.1): exactly one strong entity-tag whose opaque value is an
// integer version, If-Match: "3". A missing header, a weak tag, the * form, a
// list, or a non-integer tag is a *[PreconditionError], which the error
// mapping below answers with a 428 when the header is missing and a 400
// otherwise. The parse is syntax only; whether the version matches the row
// is the data layer's check, and a mismatch is the consumer's 412.
//
// [DecodeJSON] reads a request body strictly as one JSON value: bounded at
// the caller's limit, unknown fields rejected so a misspelled field cannot
// silently change a command's meaning, and nothing after the first value.
// A body that fails any of these, or is empty, is a *[BodyError], answered
// with a 413 when the body is over its limit and a 400 otherwise. The decode
// is syntax and shape only; the values' validity is the command's own check.
//
// # Error mapping
//
// An [ErrorWriter] turns a handler's returned error into a problem response
// through a composed [StatusMatcher] list: the package's own vocabulary is
// built in (*[QueryError] is a 400; *[PreconditionError] a 428 or a 400;
// *[BodyError] a 413 or a 400), the consumer's matchers decide the rest
// in order, first match wins, and an unclaimed error is a 500 — so HTTP
// status policy stays with the application, and the SDK depends on no
// infrastructure library's error types. The detail member carries the error
// text only on a status in the writer's detail set — 400, 413, and 428 built
// in, the statuses that are request-shaped by construction — so no internal
// error's text reaches the wire; [ErrorWriter.Detail] adds statuses for a
// surface whose clients need the reason, such as an operator API's 409.
//
// # Problem responses
//
// Error responses are RFC 9457 problem documents. The type member identifies
// the problem's semantics and is the member a client branches on, with title
// advisory and status an advisory copy of the status line. This package defines
// no type URIs of its own: every problem it emits is [ProblemTypeBlank], and a
// consumer supplies its own URI through [Problem.Write] or the extras map of
// [WriteProblemWith]. Extras may add or override any member except status,
// which always matches the status line. A zero Status defaults to 500. An empty
// title defaults to the status phrase, and is omitted for a code outside the
// standard table.
package web
