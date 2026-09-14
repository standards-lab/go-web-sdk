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
// # Correlation
//
// [RequestID] gives every request a correlation id, chosen by the first of
// three sources that yields one: a [WithIDSource] function (the seam an
// infrastructure library fills with its own trace id), an inbound header
// explicitly trusted through [WithTrustedHeader], or a generated id, 16
// bytes from crypto/rand hex-encoded to the shape of an OpenTelemetry trace
// id. No inbound header is trusted unless named. The id lands on the
// request's context through [web.WithRequestID], where [web.Problem.WriteFor]
// and this package's own log records read it back, and on the
// [RequestIDHeader] response header, set before the next handler runs so it
// survives whoever writes the response, [Recoverer] included.
//
// RequestID must sit outside RequestLogger in the chain, earlier in
// [web.Chain]'s argument list, so it runs first, for the log record to
// carry either the id or the matched route: it forwards a derived request
// through http.Request.WithContext, and only the request that reaches the
// mux gets Pattern set on it. RequestLogger reads both from the request it
// was itself given, unchanged.
//
// # Request logging
//
// [RequestLogger] emits one record per request through a *slog.Logger the
// caller supplies, at info level, with the request's method, path, matched
// route, status, duration, remote address, and correlation id. The
// attributes take OpenTelemetry's semantic-convention names
// (http.request.method, url.path, http.route, http.response.status_code,
// client.address), so an observability layer reads them without a rename;
// duration, which the conventions do not name as a log attribute, and
// request_id keep the SDK's own names. http.route and request_id appear
// only when the request matched a ServeMux pattern and carries an id
// respectively; RequestLogger documents the chain order each needs.
// [Recoverer] and [web.Handle] name the request the same way in their own
// failure records. A successful request to [web.HealthPath] or
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
// [web.WriteProblem]. A response already committed before the panic cannot
// be answered with a problem document, so Recoverer logs the committed
// status and re-raises the panic as [http.ErrAbortHandler] instead —
// net/http ends the connection rather than completing a truncated body as
// if it were a clean response. A panic that is already ErrAbortHandler is
// re-raised untouched and not logged, preserving net/http's own
// silent-abort mechanism.
//
// RequestLogger and Recoverer wrap the ResponseWriter through
// [web.WrapWriter], sharing one [web.Recorder] per request regardless of
// which wraps which first. The wrapper records the first status written
// (a 1xx status other than 101 excepted, matching net/http's own
// treatment of those as informational), implements Unwrap and FlushError
// so flushing and hijacking work through http.ResponseController and a
// Flush commits like a Write would, and delegates io.ReaderFrom so a
// handler serving files keeps the zero-copy path. http.Pusher is not
// available through the wrapper, and a Hijack bypasses it entirely, the
// same as it bypasses net/http's own response bookkeeping.
//
// # Timeout
//
// [Timeout] gives the next handler a context deadline and nothing else: no
// response of its own, no second goroutine racing the handler, no
// ResponseWriter of its own. A handler that ignores its context runs to
// completion and answers as it pleases; one that observes the deadline,
// directly or through a downstream call that respects context
// cancellation, decides for itself what to write. It is not net/http's
// TimeoutHandler.
//
// # Fixed headers
//
// [Headers] sets a caller-supplied set of response headers before the next
// handler runs, so they survive whoever writes the response, [Recoverer]
// included, the same timing RequestID uses for its own header. It knows
// nothing about what it sets; the security headers and the no-store
// Cache-Control an API's responses want are the caller's own map.
//
// # Conditional application
//
// [Maybe] applies one middleware only when a predicate on the request
// holds, composing that middleware once at wiring time rather than per
// request, so a route can gate part of its traffic — a body limit on
// writes, a header on browser-facing responses — without splitting the
// route in two.
//
// # Request shape
//
// [ContentType] rejects a request whose Content-Type is outside a
// caller-named allowlist with a 415 problem, matching on the parsed media
// type so a charset parameter never defeats it. The gate judges every
// request that reaches it; scoping it to the methods that carry a body
// composes with [Maybe]. [BodyLimit] bounds a request body through
// http.MaxBytesReader, so a handler decoding it with [web.DecodeJSON] gets a
// 413 by composition alone; a handler reading the body some other way maps
// the resulting *http.MaxBytesError itself. The close-after-response signal
// MaxBytesReader offers reaches only net/http's own ResponseWriter, so
// BodyLimit belongs outside RequestLogger and Recoverer in the chain to
// receive it.
package middleware
