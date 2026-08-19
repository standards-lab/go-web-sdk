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
// caller supplies — method, path, status, duration, and remote address — at
// info level, or at error level with the panic value attached when the
// handler panics (the panic then continues to net/http's recovery). A
// successful request to [web.HealthPath] or [web.ReadyPath] logs at debug —
// orchestrator heartbeat, visible in development and quiet in production —
// while a failing probe stays at info. Beyond that the middleware does not
// judge status codes: whether a 5xx was the application's own failure belongs
// to the error mapping, not here.
//
// The middleware wraps the ResponseWriter to capture the status. The wrapper
// records the first status written, implements Unwrap so flushing and
// hijacking work through http.ResponseController, and delegates io.ReaderFrom
// so a handler serving files keeps the zero-copy path. http.Pusher is not
// available through the wrapper.
package middleware
