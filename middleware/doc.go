// Package middleware holds the SDK's middleware implementations. Each
// constructor returns a [web.Middleware], composed with web.Chain or attached
// to a router, a group, or a route; the type and the composer stay in the
// web package, which consumes them. This comment lists every exported name;
// each symbol's own documentation states its contract.
//
//   - [RequestID] gives each request a correlation id, echoed as
//     [RequestIDHeader]; [WithTrustedHeader] and [WithIDSource] are the
//     [RequestIDOption] values that take it from upstream.
//   - [RequestLogger] logs one record per request.
//   - [Recoverer] turns a handler's panic into a logged 500 problem.
//   - [Timeout] gives the next handler's context a deadline.
//   - [Headers] sets fixed response headers.
//   - [Maybe] runs a middleware only for requests a predicate selects, and
//     [NotProbe] is the predicate that skips the probe endpoints.
//   - [ContentType] answers a 415 to a request outside the allowed media
//     types.
//   - [BodyLimit] bounds every request body; the reader refuses the
//     overflow.
//
// A middleware belongs to the transport, not to the capability it
// collaborates with: the request logger lives here and takes a standard
// *slog.Logger. A middleware that needs a third-party dependency is a
// sub-module of its own under middleware/, with its own go.mod and release
// tag, so a caller that does not use it never compiles that dependency.
// middleware/rate-limit is the first such sub-module.
//
// # Chain order
//
// In [web.Chain]'s argument order, outermost first:
//
//   - [BodyLimit] before RequestLogger and Recoverer: MaxBytesReader's
//     close-after-response signal reaches only net/http's own
//     ResponseWriter, which those two wrap.
//   - [RequestID] before RequestLogger: RequestID forwards a derived request,
//     and only the request that reaches the mux gets its Pattern set. The
//     logger reads the id and the route from the request it receives, so it
//     sees both only when nothing between it and the mux derives another.
//   - [Recoverer] in either order with RequestLogger: RequestLogger,
//     Recoverer, and [web.Handle] share one [web.Recorder] per request.
//
// RequestID and Headers set their response headers before the next handler
// runs, so the headers survive whoever writes the response, Recoverer
// included.
//
// # Log records
//
// RequestLogger's record and the failure records of Recoverer and
// [web.Handle] name the request with OpenTelemetry's semantic-convention
// attributes (http.request.method, url.path, http.response.status_code,
// client.address), plus request_id when the request carries one, so an
// observability layer reads them without renaming. The error mapping, not
// the logger, decides whether a 5xx was the application's own failure.
package middleware
