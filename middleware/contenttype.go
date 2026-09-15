package middleware

import (
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/standards-lab/go-web-sdk"
)

// ContentType rejects a request whose Content-Type is not one of allowed,
// answering a 415 problem document through [web.WriteProblem] instead of
// running the next handler. The header is parsed with [mime.ParseMediaType]
// and only the media type is compared, so "application/json; charset=utf-8"
// matches an allowed "application/json"; a raw string comparison would have
// rejected it. Media types are case-insensitive and the comparison is too.
//
// The check is unconditional: every request through the middleware is
// judged by its Content-Type, whatever its method, and one with no
// Content-Type header, or one whose header does not parse as a media type,
// is not in the allowlist and gets the 415. A body-less request (a GET, a
// DELETE) carries no Content-Type as a rule, so a ContentType hung on a
// mixed route rejects its reads. Scoping the gate to the requests that carry
// a body is [Maybe]'s job, not this middleware's:
//
//	isWrite := func(r *http.Request) bool {
//		return r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch
//	}
//	g.Use(middleware.Maybe(middleware.ContentType("application/json"), isWrite))
//
// The 415 is otherwise undecorated (about:blank, the status phrase as title,
// the request path as instance); its detail names the media type the
// request sent and the types accepted, so a client can see which of the two
// sides is wrong. The received type is the parsed one, not the raw header,
// and an unparsable header is described rather than echoed.
//
// ContentType panics with no allowed types, since nothing could ever pass,
// and on an allowed entry that is not a bare type/subtype media type: an
// entry that does not parse can never match, and one carrying parameters
// ("application/json; charset=utf-8") would match without them, which is
// not what it says. Both are wiring mistakes.
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
