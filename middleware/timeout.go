package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

// Timeout gives the next handler's context a deadline d away and nothing
// else; the handler decides what to write. It panics on a non-positive d.
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
