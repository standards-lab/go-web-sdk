package middleware

import (
	"net/http"

	"github.com/standards-lab/go-web-sdk"
)

// BodyLimit caps every request body at n bytes: the next handler runs with
// r.Body replaced by [http.MaxBytesReader] over the original, so a read past
// the limit fails with a *[http.MaxBytesError] and, when the middleware holds
// net/http's own ResponseWriter, the connection is marked to close after the
// response rather than drain the rest of the body. The body is replaced in
// place, on the request the handler receives, which is how MaxBytesReader is
// meant to be applied; nothing is cloned and nothing is read here.
//
// The middleware writes no response of its own. A handler that decodes the
// body through [web.DecodeJSON] needs no glue: DecodeJSON classifies the
// reader's overflow as a *[web.BodyError] with TooLarge set, which
// [web.ErrorWriter] answers with a 413 problem document, so a handler
// adapted by [web.Handle] answers an oversized body with a 413 by
// composition alone. DecodeJSON bounds the body with its own MaxBytesReader
// as well; the tighter of the two limits is the one a body hits, and the
// problem's detail names DecodeJSON's limit either way, since that is the
// one the decoder was given. A handler that reads the body some other way
// (io.ReadAll, its own decoder, a multipart parser) gets the plain
// *http.MaxBytesError from the read and maps it to a response itself; the
// middleware does not turn that error into a 413 on the handler's behalf.
//
// Wire BodyLimit outside the middleware that wrap the ResponseWriter
// ([Recoverer], [RequestLogger]) when the close-after-response hint matters:
// MaxBytesReader delivers it by a type assertion on the writer it is given,
// which a wrapper does not satisfy.
//
// BodyLimit panics on a limit of zero or less: [http.MaxBytesReader] treats
// both as a limit of zero, so every request with a body would be rejected on
// its first read. That is a wiring mistake, and a middleware that quietly
// applied it would fail every body it saw.
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
