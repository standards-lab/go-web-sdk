package middleware

import (
	"net/http"

	"github.com/standards-lab/go-web-sdk"
)

// header is one fixed response header, its name canonicalized at
// construction.
type header struct {
	name, value string
}

// Headers sets each entry of headers on the response before the next
// handler runs. It knows nothing about what it is setting: security headers
// and Cache-Control: no-store for an API's responses are the usual cargo,
// but the map is applied as given. Names are canonicalized the way
// [http.Header.Set] canonicalizes them, so "cache-control" and
// "Cache-Control" name the same header.
//
// The headers go on before the handler runs for the same reason [RequestID]
// sets its header first: response headers go to the wire with the first
// write, so setting them before the handler keeps them on the response
// whoever ends up writing it, including a [Recoverer] answering a panic. A
// handler that sets the same header afterwards overrides the fixed value,
// and one that deletes it removes it; the middleware sets, it does not
// enforce.
//
// Each entry is set independently, so the order of application does not
// matter and none is imposed. Headers copies the map at construction; a
// later change to the caller's map has no effect on the middleware.
//
// A nil or empty map is harmless wiring and applies nothing. Headers panics
// on an empty name, which names no header, and on two names that
// canonicalize to the same header ("cache-control" beside "Cache-Control"):
// map iteration order would then pick the winner at random per request.
func Headers(headers map[string]string) web.Middleware {
	fixed := make([]header, 0, len(headers))
	seen := make(map[string]bool, len(headers))
	for name, value := range headers {
		if name == "" {
			panic("middleware: Headers requires a name for every header")
		}
		canonical := http.CanonicalHeaderKey(name)
		if seen[canonical] {
			panic("middleware: Headers has two entries for " + canonical)
		}
		seen[canonical] = true
		fixed = append(fixed, header{canonical, value})
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			for _, f := range fixed {
				h.Set(f.name, f.value)
			}
			next.ServeHTTP(w, r)
		})
	}
}
