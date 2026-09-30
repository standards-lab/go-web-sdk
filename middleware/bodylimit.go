package middleware

import (
	"net/http"

	"github.com/standards-lab/go-web-sdk"
)

// BodyLimit bounds every request body at n bytes with [http.MaxBytesReader]
// and writes no response: [web.DecodeJSON] answers the overflow as a 413
// naming the tighter of its limit and n, and another reader maps the
// *[http.MaxBytesError] itself. It panics on a limit of zero or less.
func BodyLimit(n int64) web.Middleware {
	if n <= 0 {
		panic("middleware: BodyLimit requires a positive limit")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
