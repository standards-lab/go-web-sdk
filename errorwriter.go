package web

import (
	"errors"
	"log/slog"
	"net/http"
)

// ProblemMatcher maps an error to the problem response that reports it. It
// reports false for an error it does not recognize, passing the decision to
// the next matcher. A matcher that only cares about the status returns a
// Problem with Status set and every other member zero — Type and Title take
// [Problem.Write]'s defaults and Instance takes [Problem.WriteFor]'s, the
// same as an unclaimed error does.
type ProblemMatcher func(error) (Problem, bool)

// ErrorWriter turns a handler's returned error into an RFC 9457 problem
// response. The mappings it owns are this package's own vocabulary — a
// *[QueryError] is a 400, a *[PreconditionError] a 428 when the header is
// missing and a 400 otherwise, a *[BodyError] a 413 when the body is over
// its limit and a 400 otherwise, an *[UploadError] a 415, 411, or 413;
// every other problem is decided by the consumer's matchers, so problem
// policy stays with the application and the SDK depends on no
// infrastructure library's error types.
type ErrorWriter struct {
	matchers []ProblemMatcher
	detail   map[int]struct{}
	logger   *slog.Logger
}

// NewErrorWriter composes the matchers into a writer, wired once at route
// setup. They are consulted in argument order after the built-in matches,
// first match wins; an error no matcher claims is a 500.
func NewErrorWriter(matchers ...ProblemMatcher) *ErrorWriter {
	return &ErrorWriter{
		matchers: matchers,
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
// member, for a surface whose clients need the reason: an operator API
// reporting which schema version is dirty on a 409, or that this
// environment does not seed on a 403. The built-in set is 400, 411, 413,
// 415, and 428, the statuses that are request-shaped by construction;
// Detail only ever adds to it, and the writer does not second-guess a
// status the consumer names. Called at wiring time, like [Group.Use].
func (ew *ErrorWriter) Detail(statuses ...int) {
	for _, s := range statuses {
		ew.detail[s] = struct{}{}
	}
}

// Log sets the logger the writer reports to. The writer logs the cause of
// every 500 it writes, since the response carries no detail and the cause
// would otherwise be lost. It also logs an error it cannot write: one that a
// handler adapted by [Handle] returned after committing its response, or a
// problem whose body the encoder failed to write. Unset, the writer reports
// through slog's default logger.
// Called at wiring time, like [ErrorWriter.Detail].
func (ew *ErrorWriter) Log(logger *slog.Logger) {
	ew.logger = logger
}

func (ew *ErrorWriter) log() *slog.Logger {
	if ew.logger == nil {
		return slog.Default()
	}
	return ew.logger
}

// Problem maps err to the problem that reports it, without writing a
// response: this package's own errors first, then the matchers in argument
// order, first match wins. An error no matcher claims, and a matcher that
// claims one without naming a status, is a 500. The document is
// undecorated — Instance is empty and [ErrorWriter.Detail]'s rule has not
// been applied yet; [ErrorWriter.Write] does both.
func (ew *ErrorWriter) Problem(err error) Problem {
	if own, ok := errors.AsType[statusError](err); ok {
		return Problem{Status: own.status()}
	}
	for _, match := range ew.matchers {
		if p, ok := match(err); ok {
			if p.Status == 0 {
				p.Status = http.StatusInternalServerError
			}
			return p
		}
	}
	return Problem{Status: http.StatusInternalServerError}
}

// Status maps err to its HTTP status without writing a response, for a
// caller that needs the code alone.
func (ew *ErrorWriter) Status(err error) int {
	return ew.Problem(err).Status
}

// Write sends err as the problem [ErrorWriter.Problem] maps it to. A
// matcher that supplied its own Detail keeps it; otherwise the detail
// carries the error text only on a status in the writer's detail set (400,
// 411, 413, 415, and 428 built in, plus whatever [ErrorWriter.Detail]
// added), where it is request-shaped and client-actionable — every other
// status sends no detail, so an internal error's text never reaches the
// wire by default. Write logs a 500's cause instead, at error level
// through [ErrorWriter.Log]'s logger, with the attributes [Handle]'s
// failure records carry. No other status is logged, since each reports the
// client's error or a named condition.
// The returned error is the encoder's, as from [Problem.WriteFor].
func (ew *ErrorWriter) Write(w http.ResponseWriter, r *http.Request, err error) error {
	p := ew.Problem(err)
	if p.Status == http.StatusInternalServerError {
		ew.logCause(r, err)
	}
	if p.Detail == "" {
		if _, ok := ew.detail[p.Status]; ok {
			p.Detail = err.Error()
		}
	}
	return p.WriteFor(w, r)
}

// logCause reports the error behind a 500, which the response withholds,
// with the attribute set [Handle]'s failure records carry.
func (ew *ErrorWriter) logCause(r *http.Request, err error) {
	ew.log().LogAttrs(
		r.Context(),
		slog.LevelError,
		"internal server error",
		handleAttrs(r, http.StatusInternalServerError, err)...,
	)
}
