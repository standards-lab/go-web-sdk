// Package web provides the HTTP layer: a net/http server bound to a
// caller-supplied handler, routing, RFC 9457 problem responses, the read and
// request helpers, and the liveness and readiness endpoints an orchestrator
// probes. This comment places every exported name; each symbol's own
// documentation states its contract.
//
// # Wiring
//
// An application is wired once, before it serves, and a wiring mistake
// panics then, with the fix named, rather than failing a request later: an
// unfinalized [Config], a nil logger or writer, a malformed prefix, a
// duplicate pattern, a mutation of a group [NewModule] sealed. A constructor
// takes what the value cannot work without, including the *slog.Logger it
// reports through; optional behavior is set by a method called while the
// application is wired: [Group.Use], [Group.Mount], [Group.SetErrorWriter],
// [Group.SetNotFound], [Group.SetMethodNotAllowed], [Router.Use],
// [Router.SetNotFound], [Router.SetMethodNotAllowed], and
// [ErrorWriter.Detail]. Configuration accumulated from files and the
// environment is a struct instead, the way [Config] loads through go-core's
// config package.
//
// # Server lifecycle
//
// [Server.Start] binds on the calling goroutine and only then serves in the
// background, so a bind failure is returned rather than lost, and
// [Server.Err] reports a serve failure after it. Start and [Server.Shutdown]
// match go-core's [lifecycle.Service] members, so a composition root declares
// the server in the root stage, where every numbered stage starts beneath it
// and the drain empties it first:
//
//	lc.Add(lifecycle.Service{
//		Name:     "server",
//		Stage:    lifecycle.StageRoot,
//		Start:    srv.Start,
//		Shutdown: srv.Shutdown,
//	})
//	lc.Monitor(srv.Err())
//
// That root stage is also what makes [RegisterHealth]'s live readiness safe:
// no request reaches the probe before every check it reads exists.
//
//   - [Config] is the server's address, timeouts, and header limit; its
//     pointer fields are tri-state, nil taking the default at Finalize and
//     an explicit zero surviving, and [Env] records the override names
//     [Config.Finalize] composed.
//   - [NewServer] builds the [Server] from a finalized Config, a handler, and
//     the logger net/http's own diagnostics go to.
//   - [Liveness] and [Readiness] are the probe handlers, [RegisterHealth]
//     mounts them at [HealthPath] and [ReadyPath] over a lifecycle
//     coordinator, and a [Mounter] is anything they mount on, an
//     http.ServeMux or a [Router].
//
// # Routing
//
// A [Group] declares routes under a path prefix, with a middleware stack
// and nested child groups; [NewModule] compiles a group tree once into a
// [Module], and a [Router] dispatches to modules by longest prefix on
// segment boundaries, falling back to a native http.ServeMux, where
// [Router.Handle] mounts the probes outside every module's middleware. The
// effective middleware order is [Router.Use], then group middleware root to
// leaf, then route middleware, then the handler. Nothing recomposes per
// request.
//
// A request no route matches is answered with a 404 problem, and one whose
// method does not match with a 405 problem carrying Allow, in place of
// ServeMux's plain text; its redirects are served unchanged. A miss reaches
// no route, so only Router.Use middleware sees it.
//
//   - [NewGroup] opens a [Group]; [Group.Handle] and [Group.HandleErr]
//     declare its routes.
//   - [NewModule] compiles a group tree, and [NewHandlerModule] mounts a raw
//     handler, such as an embedded client, as a [Module].
//   - [NewRouter] returns the [Router] that dispatches to modules.
//   - [Middleware] is the wrapper type every layer takes, and [Chain]
//     composes it outermost first; the middleware package holds the
//     implementations.
//   - [Recorder] records whether and with what status a response committed,
//     and [WrapWriter] shares one per request among the layers that need it.
//
// # Problems
//
// Every error response is an RFC 9457 problem document. The type member is
// the one a client branches on; this package mints no type URIs, so every
// problem it emits itself is [ProblemTypeBlank], and a consumer names its
// own through a [Problem]'s Type. A handler written as a [HandlerFunc]
// returns its error and [Handle] writes it through an [ErrorWriter]: this
// package's own errors ([QueryError], [PreconditionError], [BodyError],
// [UploadError], [PathError]) and a returned Problem map themselves, and
// the consumer's [ProblemMatcher] list decides the rest, so problem policy
// stays with the application and the SDK depends on no infrastructure
// library's errors. An error nothing claims is a 500, whose cause is logged
// and withheld from the wire. When the request carries a correlation id
// ([WithRequestID]), every problem written for it carries the id as its
// "request_id" member.
//
//   - [Problem] is the document, served as [ProblemMediaType];
//     [Problem.WriteFor] and [WriteProblem] send one for a request.
//   - [NewErrorWriter] builds the [ErrorWriter] from its logger and
//     matchers; it logs the cause of every 5xx it sends, a 503 at warn, the
//     client's own cancellation at debug, and every other 5xx at error.
//   - [HandlerFunc] is the error-returning handler, and [Handle] adapts one
//     over an ErrorWriter.
//   - [WithRequestID] and [RequestIDFrom] carry the correlation id in a
//     request's context.
//   - [WriteJSON] sends a success body as [JSONMediaType].
//
// # Reads
//
// [ParseQuery] parses a read request's query string in full into a [Query]:
// the page, size, sort, and cursor parameters, and every other parameter as
// a [Filter], so a handler cannot parse the paging and forget to strip it
// from the filters. Sort is comma-separated field names, "-" prefixing a
// descending key ("sort=name,-code"). A filter is a field name with an
// optional bracketed operator ("status=active", "created[gte]=2026-01-01").
// Names and operators are lexical here: whether a field is readable or an
// operator supported is the data layer's check. A read is addressed by
// number (page=3) or by the cursor a previous page's Next carried, never
// both, and only a read whose [Limits] opt in takes a cursor. [NewPage]
// assembles the [Page] envelope from the items and the read's [Paging].
//
//   - [Limits] is a read's paging policy, and [ParseQuery] parses under it
//     into a [Query] of [Sort] keys and [Filter] parameters, refusing with a
//     [QueryError].
//   - [Paging] is what the data layer reports beyond the items, and
//     [NewPage] assembles the [Page] envelope from both.
//
// # Requests and objects
//
// [IfMatch], [DecodeJSON], [ReadUpload], and [PathUUID] read a request's
// precondition, JSON body, raw upload, and UUID path value, each rejecting
// with an error the ErrorWriter maps. [WriteObject] proxies a stored object
// as the response rather than redirecting to a presigned URL, which would
// hand out a bearer credential the service cannot revoke and bypass its
// authorization; [Attachment] makes it a download.
//
//   - [IfMatch] reads the version a guarded command names, refusing with a
//     [PreconditionError].
//   - [DecodeJSON] reads a strict JSON body, refusing with a [BodyError].
//   - [ReadUpload] accepts a raw body by its headers as an [Upload],
//     refusing with an [UploadError].
//   - [PathUUID] reads a path value as a canonical UUID, refusing with a
//     [PathError].
//   - [WriteObject] proxies an [Object]'s bytes with its validators, and
//     [Attachment] builds the Content-Disposition of a download.
package web
