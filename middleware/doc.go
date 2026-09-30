// Package middleware holds the SDK's middleware implementations: [RequestID],
// [RequestLogger], [Recoverer], [Timeout], [Headers], [Maybe], [ContentType],
// and [BodyLimit]. Each constructor returns a [web.Middleware], composed with
// web.Chain or hung on a router, a group, or a route; the type and the
// composer stay in the web package, which consumes them.
//
// A middleware belongs to the transport, not to the capability it
// collaborates with: the request logger lives here and takes a standard
// *slog.Logger. A middleware that needs a third-party dependency is a
// sub-module of its own under middleware/, with its own go.mod and release
// tag, so a caller that does not use it never compiles that dependency;
// middleware/rate-limit is the first.
//
// # Chain order
//
// In [web.Chain]'s argument order, outermost first:
//
//   - [BodyLimit] before RequestLogger and Recoverer: MaxBytesReader's
//     close-after-response signal reaches only net/http's own
//     ResponseWriter, which those two wrap.
//   - [RequestID] before RequestLogger: it forwards a derived request, and
//     only the request that reaches the mux gets its Pattern set, so the
//     logger reads the id and the route from its own request only when
//     nothing between it and the mux derives one.
//   - [Recoverer] in either order with RequestLogger: RequestLogger,
//     Recoverer, and [web.Handle] share one [web.Recorder] per request.
//
// RequestID and Headers set their response headers before the next handler
// runs, so the headers survive whoever writes the response, Recoverer
// included.
//
// # Log records
//
// RequestLogger's record and Recoverer's and [web.Handle]'s failure records
// name the request by OpenTelemetry's semantic conventions
// (http.request.method, url.path, http.response.status_code,
// client.address), so an observability layer reads them without a rename,
// plus request_id when the request carries one. Whether a 5xx was the
// application's own failure belongs to the error mapping, not to the
// logger.
package middleware
