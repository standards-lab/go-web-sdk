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

// WithIDSource supplies the function [RequestID] asks first, the seam a
// tracing layer fills with the request's trace id; "" declines. It panics
// on a nil fn.
func WithIDSource(fn func(*http.Request) string) RequestIDOption {
	if fn == nil {
		panic("middleware: WithIDSource requires a non-nil source")
	}
	return func(c *requestIDConfig) { c.source = fn }
}

// WithTrustedHeader names an inbound header whose value [RequestID] echoes
// as the id, for a deployment whose reverse proxy assigns one; no header is
// trusted otherwise. It panics on an empty name.
func WithTrustedHeader(name string) RequestIDOption {
	if name == "" {
		panic("middleware: WithTrustedHeader requires a header name")
	}
	return func(c *requestIDConfig) { c.header = name }
}

// RequestID gives every request a correlation id, the first of these that
// yields one:
//
//  1. The source function from [WithIDSource].
//  2. The inbound header named by [WithTrustedHeader], when its value is 1
//     to 128 visible ASCII characters.
//  3. A generated id: 16 bytes from crypto/rand, hex-encoded, the shape of an
//     OpenTelemetry trace id.
//
// The id is set on the request's context ([web.WithRequestID]) and the
// [RequestIDHeader] response header before the next handler runs.
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
		if id := r.Header.Get(c.header); validID(id) {
			return id
		}
	}
	return generateID()
}

// validID reports whether an inbound id is 1 to 128 visible ASCII
// characters, a shape safe to echo into a log record and a response header.
func validID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i := range len(id) {
		if id[i] < '!' || id[i] > '~' {
			return false
		}
	}
	return true
}

// generateID returns 32 lowercase hex characters from 16 random bytes.
// crypto/rand.Read never returns an error: it crashes the program
// irrecoverably if the operating system's source fails.
func generateID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
