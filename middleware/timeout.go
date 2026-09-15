package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

// Timeout gives every request a context deadline d from the moment the
// middleware sees it: the next handler runs with [context.WithTimeout]
// applied to the request's context, and the timer is cancelled when the
// handler returns, releasing its resources early for a request that
// finishes in time.
//
// That is all it does. The deadline is for the handler to observe, through
// r.Context().Done() or a downstream operation that respects context
// cancellation (a database call, an outbound request); Timeout writes no
// response of its own, does not run the handler on another goroutine or
// race it against the clock, and does not touch the ResponseWriter. It is
// not [http.TimeoutHandler]: a handler that ignores its context runs to
// completion and answers as it pleases, and one that notices the deadline
// decides for itself what to write (typically a 503 or 504 problem). The
// request logger records whatever status that turns out to be.
//
// Timeout panics on a duration of zero or less: [context.WithTimeout] with
// a non-positive duration yields a context that is already expired, so every
// request through the middleware would start cancelled. That is a wiring
// mistake, and a middleware that quietly applied it would fail every
// request it saw.
func Timeout(d time.Duration) web.Middleware {
	if d <= 0 {
		panic("middleware: Timeout requires a positive duration")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
