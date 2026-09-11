// Package middleware holds the SDK's middleware implementations. Each is a
// constructor returning a [web.Middleware], composed around a handler with
// web.Chain or hung on a router, a group, or a route; the type and the
// composer stay in the web package, which consumes them.
//
// A middleware belongs to the transport, not to the capability it
// collaborates with: the request logger lives here and takes a standard
// *slog.Logger rather than living in a logging package and taking an HTTP
// type. The capability supplies the collaborator; this package supplies the
// middleware that consumes it.
//
// # Request logging
//
// [RequestLogger] emits one record per request through a *slog.Logger the
// caller supplies: the method, path, status, duration, and remote address,
// at info level. A successful request to [web.HealthPath] or
// [web.ReadyPath] logs at debug: orchestrator heartbeat, visible in
// development and quiet in production. A failing probe stays at info.
// Beyond that the middleware does not judge status codes: whether a 5xx was
// the application's own failure belongs to the error mapping, not here.
//
// RequestLogger does not recover panics; [Recoverer] does, in either chain
// order. When a panic unwinds through RequestLogger with nothing
// committed, the record's status is 500 — what a Recoverer outside it goes
// on to write, or, with none wired, the failure net/http's own recovery
// leaves the client with. The panic value itself belongs to whichever
// recovery point catches it, not to this record.
//
// # Recovery
//
// [Recoverer] is the chain's recovery point: it recovers a handler panic,
// logs the value and a stack trace at error level, and, if the handler had
// not committed a response, writes a 500 problem document through
// [web.WriteProblem]. A response already committed before the panic is
// left alone — the client gets what was sent, and the record carries that
// status instead. [http.ErrAbortHandler] passes through untouched,
// preserving net/http's own silent-abort mechanism.
//
// RequestLogger and Recoverer wrap the ResponseWriter through
// [web.WrapWriter], sharing one [web.Recorder] per request regardless of
// which wraps which first. The wrapper records the first status written,
// implements Unwrap so flushing and hijacking work through
// http.ResponseController, and delegates io.ReaderFrom so a handler
// serving files keeps the zero-copy path. http.Pusher is not available
// through the wrapper.
package middleware
