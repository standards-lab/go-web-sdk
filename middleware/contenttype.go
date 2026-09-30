package middleware

import (
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/standards-lab/go-web-sdk"
)

// ContentType answers a 415 problem, naming allowed, to a request of any
// method whose media type is not one of them. It panics on no types or one
// that is not a bare type/subtype. [Maybe] scopes it to writes:
//
//	isWrite := func(r *http.Request) bool {
//		return r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch
//	}
//	g.Use(middleware.Maybe(middleware.ContentType("application/json"), isWrite))
func ContentType(allowed ...string) web.Middleware {
	if len(allowed) == 0 {
		panic("middleware: ContentType requires at least one allowed media type")
	}
	types := make([]string, 0, len(allowed))
	for _, a := range allowed {
		mt, params, err := mime.ParseMediaType(a)
		if err != nil || len(params) > 0 || !strings.Contains(mt, "/") {
			panic(fmt.Sprintf("middleware: ContentType requires a bare type/subtype media type, got %q", a))
		}
		if !slices.Contains(types, mt) {
			types = append(types, mt)
		}
	}
	accepted := strings.Join(types, ", ")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("Content-Type")
			if raw == "" {
				reject(w, r, "the request has no Content-Type header", accepted)
				return
			}
			mt, _, err := mime.ParseMediaType(raw)
			if err != nil {
				reject(w, r, "the Content-Type header is not a media type", accepted)
				return
			}
			if !slices.Contains(types, mt) {
				reject(w, r, "Content-Type "+mt+" is not accepted", accepted)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// reject writes the 415 for ContentType: why the request's Content-Type
// failed, and the types that would have passed. The encoder's error is
// dropped, as the router's own miss handlers drop theirs; nothing here logs.
func reject(w http.ResponseWriter, r *http.Request, why, accepted string) {
	_ = web.WriteProblem(
		w,
		r,
		http.StatusUnsupportedMediaType,
		"",
		why+"; accepted: "+accepted,
	)
}
