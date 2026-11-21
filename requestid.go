package web

import "context"

const requestIDMember = "request_id"

type requestIDKey struct{}

// WithRequestID returns a context carrying id as the request's correlation
// id, which [Problem.WriteFor] writes as the "request_id" member of every
// problem for the request.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFrom reports the id ctx carries and whether one was set; an id
// set to "" reports "", true.
func RequestIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey{}).(string)
	return id, ok
}
