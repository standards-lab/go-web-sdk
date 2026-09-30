package middleware

import (
	"net/http"

	"github.com/standards-lab/go-web-sdk"
)

// Maybe applies mw only to a request pred holds for, choosing per request
// between two handlers composed once. It panics on a nil mw or pred.
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

// NotProbe is [Maybe]'s predicate excluding [web.HealthPath] and
// [web.ReadyPath], so a middleware that judges a request, such as a rate
// limit, never answers an orchestrator's probe in the handler's place.
func NotProbe(r *http.Request) bool {
	return r.URL.Path != web.HealthPath && r.URL.Path != web.ReadyPath
}
