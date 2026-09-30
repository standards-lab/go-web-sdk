package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
)

// ProblemMatcher maps an error to the problem that reports it, or reports
// false to pass it to the next matcher. A Problem with only Status set takes
// [Problem.WriteFor]'s defaults for the rest.
type ProblemMatcher func(error) (Problem, bool)

// ErrorWriter turns a handler's returned error into an RFC 9457 problem
// response: this package's own error types and a returned [Problem] map
// themselves, and the consumer's matchers decide the rest, so problem policy
// stays with the application. A Problem maps itself only when it is the
// returned error, a Problem or a *Problem; one wrapped inside another error
// reaches the matchers like any other error.
type ErrorWriter struct {
	matchers []ProblemMatcher
	detail   map[int]struct{}
	logger   *slog.Logger
}

// NewErrorWriter returns a writer that consults matchers in argument order
// after the built-in mappings; the first match wins. logger receives the
// cause of every 5xx the writer sends and every error it cannot write. It
// panics on a nil logger.
func NewErrorWriter(logger *slog.Logger, matchers ...ProblemMatcher) *ErrorWriter {
	if logger == nil {
		panic("web: NewErrorWriter requires a *slog.Logger")
	}
	return &ErrorWriter{
		matchers: matchers,
		logger:   logger,
		detail: map[int]struct{}{
			http.StatusBadRequest:            {},
			http.StatusLengthRequired:        {},
			http.StatusRequestEntityTooLarge: {},
			http.StatusUnsupportedMediaType:  {},
			http.StatusPreconditionRequired:  {},
		},
	}
}

// Detail adds statuses whose problems carry the error text as their detail
// member when no matcher supplied one, for a surface whose clients need the
// reason, such as an operator API's 409. The built-in set is 400, 411, 413,
// 415, and 428, the statuses that are request-shaped by construction; every
// other status sends no error text, so an internal error's text never
// reaches the wire by default.
func (ew *ErrorWriter) Detail(statuses ...int) {
	for _, s := range statuses {
		ew.detail[s] = struct{}{}
	}
}

// Problem maps err to the problem that reports it, without writing it: this
// package's own errors first, anywhere in err's chain, then err itself as
// is when it is a [Problem] or a non-nil *Problem, then the matchers. An
// error nothing claims, or a claim with no status, is a 500. Instance and the
// [ErrorWriter.Detail] rule are left to [ErrorWriter.Write].
func (ew *ErrorWriter) Problem(err error) Problem {
	p, _ := ew.problem(err)
	return p
}

// problem is [ErrorWriter.Problem], also reporting whether the problem was
// returned whole, which Write sends without adding the error text.
func (ew *ErrorWriter) problem(err error) (p Problem, whole bool) {
	if own, ok := errors.AsType[statusError](err); ok {
		return Problem{Status: own.status()}, false
	}
	switch p := err.(type) {
	case Problem:
		return withStatus(p), true
	case *Problem:
		if p != nil {
			return withStatus(*p), true
		}
	}
	for _, match := range ew.matchers {
		if p, ok := match(err); ok {
			return withStatus(p), false
		}
	}
	return Problem{Status: http.StatusInternalServerError}, false
}

func withStatus(p Problem) Problem {
	if p.Status == 0 {
		p.Status = http.StatusInternalServerError
	}
	return p
}

// Write sends err as the problem [ErrorWriter.Problem] maps it to, through
// [Problem.WriteFor], so an empty Instance becomes the request path, a
// returned Problem's included. The error text becomes the detail where
// [ErrorWriter.Detail] allows, except for a returned Problem, which is sent
// with its own members. Write logs a 5xx's cause: a 503 at warn, the
// client's own cancellation at debug, and any other 5xx at error.
func (ew *ErrorWriter) Write(w http.ResponseWriter, r *http.Request, err error) error {
	p, whole := ew.problem(err)
	if p.Status >= http.StatusInternalServerError {
		ew.logCause(r, p.Status, err)
	}
	if !whole && p.Detail == "" {
		if _, ok := ew.detail[p.Status]; ok {
			p.Detail = err.Error()
		}
	}
	return p.WriteFor(w, r)
}

func (ew *ErrorWriter) logCause(r *http.Request, status int, err error) {
	level := slog.LevelError
	switch {
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		level = slog.LevelDebug
	case status == http.StatusServiceUnavailable:
		level = slog.LevelWarn
	}
	ew.logger.LogAttrs(r.Context(), level, "server error response", handleAttrs(r, status, err)...)
}
