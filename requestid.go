package web

import "context"

// requestIDMember is the extension member under which [Problem.WriteFor]
// surfaces the request's correlation id.
const requestIDMember = "request_id"

// requestIDKey is the context key for the request's correlation id. An
// unexported struct type, so no other package can collide with it: the id
// is reachable only through [WithRequestID] and [RequestIDFrom].
type requestIDKey struct{}

// WithRequestID returns a context carrying id as the request's correlation
// id, for [RequestIDFrom] to read back. The package stores the id and
// nothing more: request-scoped middleware sets it, on the request's context,
// and [Problem.WriteFor] then surfaces it as the "request_id" extension
// member of every problem document it writes for that request. Calling it
// again on the returned context replaces the id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFrom reports the request id ctx carries, and whether one was set.
// A context that never passed through [WithRequestID] reports "", false; the
// pair is a plain round-trip, so an id set to the empty string reports "",
// true, and a caller that needs a usable id checks the string as well.
func RequestIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey{}).(string)
	return id, ok
}
