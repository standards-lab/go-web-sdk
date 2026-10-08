// Package web provides the HTTP layer: a net/http server bound to a
// caller-supplied handler, routing, RFC 9457 problem responses, the read and
// request helpers, and the liveness and readiness endpoints an orchestrator
// probes. This comment lists every exported name by section; each symbol's
// own documentation states its contract.
//
// # Wiring
//
// An application is wired once, before it serves. A wiring mistake panics
// then, with the fix named, rather than failing a request later: an
// unfinalized [Config], a nil logger or writer, a nil [Doctor] passed to
// [RegisterHealth], a malformed prefix, a duplicate pattern, or a mutation
// of a group [NewModule] sealed. A constructor takes what the value cannot work without, including the
// *slog.Logger it reports through. Optional behavior is set by a method
// called during wiring: [Group.Use], [Group.Mount], [Group.SetErrorWriter],
// [Group.SetNotFound], [Group.SetMethodNotAllowed], [Router.Use],
// [Router.SetNotFound], [Router.SetMethodNotAllowed], and
// [ErrorWriter.Detail]. Configuration accumulated from files and the
// environment is a struct instead, as [Config] is, loaded through go-core's
// config package.
//
// # Server lifecycle
//
// [Server.Start] binds on the calling goroutine and only then serves in the
// background, so a bind failure is returned rather than lost, and
// [Server.Err] reports a later serve failure. A *Server is a
// lifecycle.Subsystem and a lifecycle.Monitored, so a composition root
// defines it as a graph node's value and the Coordinator starts it, shuts
// it down, and watches its Err, with no Monitor call. A [lifecycle.Readiness]
// node lets the node that mounts the probes report the Coordinator that
// lifecycle.New later binds it to:
//
//	ready := g.Define("readiness", func(*graph.Scope) (*lifecycle.Readiness, error) {
//		return new(lifecycle.Readiness), nil
//	})
//	server := g.Define("server", func(s *graph.Scope) (*web.Server, error) {
//		mux := http.NewServeMux()
//		web.RegisterHealth(mux, s.Use(ready), web.Problem{})
//		mux.Handle("/", s.Use(api))
//		return web.NewServer(cfg, mux, logger), nil
//	})
//	sys, err := g.Build(server)
//	// ...
//	err = lifecycle.New(sys, lcCfg).Run(ctx)
//
// The server node uses what it serves, so it sits in a layer above them:
// it starts after them and drains before them, and no request reaches the
// readiness probe before every check it reads exists.
//
//   - [Config] is the server's address, timeouts, header limit, and
//     transfer rate. Its pointer fields are tri-state: nil takes the default
//     at Finalize, and an explicit zero survives. [Env] records the override
//     names [Config.Finalize] composed.
//   - [NewServer] builds the [Server] from a finalized Config, a handler, and
//     the logger net/http's own diagnostics go to.
//   - [Liveness] and [Readiness] are the probe handlers. [RegisterHealth]
//     mounts them at [HealthPath] and [ReadyPath] over a [Doctor], a
//     lifecycle.Coordinator or a lifecycle.Readiness, on a [Mounter]: an
//     http.ServeMux or a [Router].
//
// # Routing
//
// A [Group] declares routes under a path prefix, with a middleware stack and
// nested child groups. [NewModule] compiles a group tree once into a
// [Module]. A [Router] dispatches to modules by longest prefix on segment
// boundaries and falls back to a native http.ServeMux, where [Router.Handle]
// mounts the probes outside every module's middleware. The effective
// middleware order is [Router.Use], then group middleware root to leaf, then
// route middleware, then the handler. No chain is recomposed per request.
//
// The router answers a request no route matches with a 404 problem, and one
// whose method does not match with a 405 problem carrying Allow, in place of
// ServeMux's plain text; ServeMux's redirects are served unchanged. A miss
// reaches no route, so only Router.Use middleware sees it.
//
//   - [NewGroup] opens a [Group]; [Group.Handle] and [Group.HandleErr]
//     declare its routes.
//   - [NewModule] compiles a group tree, and [NewHandlerModule] mounts a raw
//     handler, such as an embedded client, as a [Module].
//   - [NewRouter] returns the [Router] that dispatches to modules.
//   - [Middleware] is the wrapper type every layer takes, and [Chain]
//     composes middleware outermost first. The middleware package holds the
//     implementations.
//   - [Recorder] records whether and with what status a response committed,
//     and [WrapWriter] shares one per request among the layers that need it.
//
// # Problems
//
// Every error response is an RFC 9457 problem document. A client branches on
// its type member. This package mints no type URIs, so every problem it
// emits itself is [ProblemTypeBlank], and a consumer names its own through a
// [Problem]'s Type. A handler written as a [HandlerFunc] returns its error,
// and [Handle] writes it through an [ErrorWriter]. This package's own errors
// ([QueryError], [PreconditionError], [BodyError], [UploadError],
// [PathError]) and a returned Problem map themselves, and the consumer's
// [ProblemMatcher] list decides the rest. Problem policy therefore stays with
// the application, and the SDK depends on no infrastructure library's
// errors. An error nothing claims is a 500; its cause is logged and withheld
// from the wire. When the request carries a correlation id
// ([WithRequestID]), every problem written for it carries the id as its
// "request_id" member.
//
//   - [Problem] is the document, served as [ProblemMediaType];
//     [Problem.WriteFor] and [WriteProblem] send one for a request.
//   - [NewErrorWriter] builds the [ErrorWriter] from its logger and
//     matchers. The writer logs the cause of every 5xx it sends: a 503 at
//     warn, the client's own cancellation at debug, and every other 5xx at
//     error.
//   - [HandlerFunc] is the error-returning handler, and [Handle] adapts one
//     over an ErrorWriter.
//   - [WithRequestID] and [RequestIDFrom] carry the correlation id in a
//     request's context.
//   - [WriteJSON] sends a success body as [JSONMediaType].
//
// # Reads
//
// [ParseQuery] parses a read request's whole query string into a [Query]:
// the page, size, sort, and cursor parameters, and every other parameter as
// a [Filter], so a handler cannot parse the paging and forget to strip it
// from the filters. Sort is comma-separated field names, with "-" prefixing
// a descending key ("sort=name,-code"). A filter is a field name with an
// optional bracketed operator ("status=active", "created[gte]=2026-01-01").
// Names and operators are checked only lexically here; the data layer checks
// whether a field is readable and an operator supported. A read is addressed
// by number (page=3) or by the cursor a previous page's Next carried, never
// both, and only a read whose [Limits] opt in takes a cursor.
//
//   - [Limits] is a read's paging policy, and [ParseQuery] parses under it
//     into a [Query] of [Sort] keys and [Filter] parameters, refusing with a
//     [QueryError].
//   - [Paging] is what the data layer reports beyond the items, and
//     [NewPage] assembles the [Page] envelope from both.
//
// # Requests and objects
//
// Each request helper rejects a request with an error the [ErrorWriter]
// maps. [WriteObject] proxies a stored object as the response rather than
// redirecting to a presigned URL, which would hand out a bearer credential
// the service cannot revoke and bypass the service's authorization.
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
//   - [Transfer] sets an upload's or a download's connection deadlines from
//     the body's size and the slowest pace a client is allowed;
//     [Config.Transfer] and [NewTransfer] build one.
package web
