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

// Headers sets headers on the response before the next handler, which may
// override them. It copies the map at construction, so a later change to the
// caller's map has no effect. It panics on an empty name or two that
// canonicalize alike.
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
