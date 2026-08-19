package web

import (
	"net/http"
	"slices"
)

// Middleware wraps one http.Handler in another.
type Middleware func(http.Handler) http.Handler

// Chain composes middleware around handler in argument order: in
// Chain(h, a, b), a sees the request first. A nil entry is skipped, so a
// caller can build a chain with conditional entries without filtering it.
func Chain(handler http.Handler, middleware ...Middleware) http.Handler {
	for _, m := range slices.Backward(middleware) {
		if m == nil {
			continue
		}
		handler = m(handler)
	}
	return handler
}
