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

// RequestIDOptions configures [RequestID]. The zero value trusts nothing
// upstream and generates every id.
type RequestIDOptions struct {
	// Source is asked first for the id, such as a tracing layer's function
	// returning the request's trace id; its return is used verbatim. A
	// Source that returns "" declines, and a nil Source is unset.
	Source func(*http.Request) string

	// TrustedHeader names an inbound header whose value is echoed as the id,
	// for a deployment whose reverse proxy assigns one. The name is matched
	// case-insensitively. Empty, the default, trusts no header: any client
	// can send one.
	TrustedHeader string
}

// RequestID gives every request a correlation id, the first of these that
// yields one:
//
//  1. opts.Source's return, when Source is set and returns a non-empty id.
//  2. The inbound header named by opts.TrustedHeader, when TrustedHeader is
//     set and the header's value is 1 to 128 visible ASCII characters.
//  3. A generated id: 16 bytes from crypto/rand, hex-encoded, the shape of an
//     OpenTelemetry trace id.
//
// The id is set on the request's context ([web.WithRequestID]) and the
// [RequestIDHeader] response header before the next handler runs. RequestID
// skips a nil Source or an empty TrustedHeader and does not panic on either.
func RequestID(opts RequestIDOptions) web.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := opts.id(r)
			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(web.WithRequestID(r.Context(), id)))
		})
	}
}

// id resolves r's correlation id by the precedence RequestID documents.
func (o RequestIDOptions) id(r *http.Request) string {
	if o.Source != nil {
		if id := o.Source(r); id != "" {
			return id
		}
	}
	if o.TrustedHeader != "" {
		if id := r.Header.Get(o.TrustedHeader); validID(id) {
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
