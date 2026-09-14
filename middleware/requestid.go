package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/standards-lab/go-web-sdk"
)

// RequestIDHeader is the response header [RequestID] sets to the request's
// correlation id.
const RequestIDHeader = "X-Request-Id"

// RequestIDOption configures [RequestID].
type RequestIDOption func(*requestIDConfig)

// requestIDConfig is the settled configuration of one RequestID middleware.
type requestIDConfig struct {
	source func(*http.Request) string
	header string
}

// WithIDSource supplies the function [RequestID] asks first for a request's
// id. It is the seam a tracing layer fills: the source returns the request's
// current trace id, and the correlation id then is the trace id. A source
// returning "" declines that request, and RequestID falls through to the
// next step of its precedence.
//
// WithIDSource panics on a nil fn: a missing source is a wiring mistake,
// and a middleware that quietly generated ids in its place would run with
// ids that correlate to nothing.
func WithIDSource(fn func(*http.Request) string) RequestIDOption {
	if fn == nil {
		panic("middleware: WithIDSource requires a non-nil source")
	}
	return func(c *requestIDConfig) { c.source = fn }
}

// WithTrustedHeader names an inbound request header whose non-empty value
// [RequestID] echoes as the request's id, after the source function and
// before generating one. It is an explicit opt-in for a deployment with a
// trusted reverse proxy in front that assigns ids; without it RequestID
// trusts no inbound header, since any client can send one. The name is
// matched the way http.Header does, case-insensitively.
//
// WithTrustedHeader panics on an empty name: there is no header to trust,
// and a middleware that silently ignored the option would report a proxy's
// ids as its own generated ones.
func WithTrustedHeader(name string) RequestIDOption {
	if name == "" {
		panic("middleware: WithTrustedHeader requires a header name")
	}
	return func(c *requestIDConfig) { c.header = name }
}

// RequestID gives every request a correlation id. The id is chosen by the
// first of these that yields a non-empty string:
//
//  1. The source function from [WithIDSource], when one is configured.
//  2. The inbound header named by [WithTrustedHeader], when one is
//     configured and the request carries a non-empty value for it.
//  3. A generated id: 16 bytes from crypto/rand, hex-encoded to 32
//     lowercase characters, the shape of an OpenTelemetry trace id.
//
// With no options, RequestID always generates. It never trusts an inbound
// header it was not told to trust.
//
// The middleware sets the id on the request's context through
// [web.WithRequestID], so the handler, later middleware, and
// [web.Problem.WriteFor] all see it, and sets the [RequestIDHeader]
// response header to the same value. Both happen before the next handler
// runs: response headers go to the wire with the first write, so setting
// the header first keeps it on the response whoever ends up writing it,
// including a [Recoverer] answering a panic. A handler that deletes the
// header from the response before writing removes it; the context value
// stays.
//
// RequestID does not wrap the ResponseWriter; it has nothing to observe in
// the response.
func RequestID(opts ...RequestIDOption) web.Middleware {
	var cfg requestIDConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := cfg.id(r)
			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(web.WithRequestID(r.Context(), id)))
		})
	}
}

// id resolves r's correlation id by the precedence RequestID documents.
func (c *requestIDConfig) id(r *http.Request) string {
	if c.source != nil {
		if id := c.source(r); id != "" {
			return id
		}
	}
	if c.header != "" {
		if id := r.Header.Get(c.header); id != "" {
			return id
		}
	}
	return generateID()
}

// generateID returns 32 lowercase hex characters from 16 random bytes.
// crypto/rand.Read never returns an error: it crashes the program
// irrecoverably if the operating system's source fails.
func generateID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
