package middleware

import (
	"net/http"

	"github.com/standards-lab/go-web-sdk"
)

// Maybe applies mw to a request only when pred(r) holds; otherwise the
// request goes straight to the next handler, unwrapped. It is the way to
// hang a middleware on part of a route's traffic (a body limit on writes
// only, a header on browser-facing responses only) without splitting the
// route.
//
// Maybe composes mw around the next handler once, when the chain it sits in
// is composed: [web.Chain], and a router, group, or route applying its
// middleware, call the returned Middleware one time at wiring, and that call
// builds the wrapped handler. A request then only chooses between the two
// handlers already built; nothing recomposes per request, matching the rest
// of the package.
//
// pred runs on every request, before either handler, and sees the request
// as Maybe received it. A middleware outside Maybe that derives a request
// (as [RequestID] does) is visible to pred; one inside is not.
//
// Maybe panics on a nil mw or a nil pred: there is nothing to apply, or no
// way to decide, and a middleware that silently passed every request
// through would look wired while doing nothing.
func Maybe(mw web.Middleware, pred func(*http.Request) bool) web.Middleware {
	if mw == nil {
		panic("middleware: Maybe requires a middleware to apply")
	}
	if pred == nil {
		panic("middleware: Maybe requires a predicate")
	}
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pred(r) {
				wrapped.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
