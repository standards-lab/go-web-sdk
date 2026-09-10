package web

import (
	"errors"
	"log/slog"
	"net/http"
)

// StatusMatcher maps an error to an HTTP status. It reports false for an
// error it does not recognize, passing the decision to the next matcher.
type StatusMatcher func(error) (int, bool)

// ErrorWriter turns a handler's returned error into an RFC 9457 problem
// response. The mappings it owns are this package's own vocabulary — a
// *[QueryError] is a 400, a *[PreconditionError] a 428 when the header is
// missing and a 400 otherwise, a *[BodyError] a 413 when the body is over
// its limit and a 400 otherwise; every other status is decided by the
// consumer's matchers, so HTTP status policy stays with the application and
// the SDK depends on no infrastructure library's error types.
type ErrorWriter struct {
	matchers []StatusMatcher
	detail   map[int]struct{}
	logger   *slog.Logger
}

// NewErrorWriter composes the matchers into a writer, wired once at route
// setup. They are consulted in argument order after the built-in matches,
// first match wins; an error no matcher claims is a 500.
func NewErrorWriter(matchers ...StatusMatcher) *ErrorWriter {
	return &ErrorWriter{
		matchers: matchers,
		detail: map[int]struct{}{
			http.StatusBadRequest:            {},
			http.StatusRequestEntityTooLarge: {},
			http.StatusPreconditionRequired:  {},
		},
	}
}

// Detail adds statuses whose problems carry the error text as their detail
// member, for a surface whose clients need the reason: an operator API
// reporting which schema version is dirty on a 409, or that this
// environment does not seed on a 403. The built-in set is 400, 413, and 428,
// the statuses that are request-shaped by construction; Detail only ever
// adds to it, and the writer does not second-guess a status the consumer
// names. Called at wiring time, like [Group.Use].
func (ew *ErrorWriter) Detail(statuses ...int) {
	for _, s := range statuses {
		ew.detail[s] = struct{}{}
	}
}

// Log sets the logger the writer reports to when an error cannot be
// written: a handler adapted by [Handle] that returned an error after
// committing its response. Unset, the writer reports through slog's default logger.
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

// Status maps err to its HTTP status without writing a response, for a
// caller that needs the code alone.
func (ew *ErrorWriter) Status(err error) int {
	if own, ok := errors.AsType[statusError](err); ok {
		return own.status()
	}
	for _, match := range ew.matchers {
		if status, ok := match(err); ok {
			return status
		}
	}
	return http.StatusInternalServerError
}

// Write sends err as a problem at [ErrorWriter.Status]'s mapping. The detail
// carries the error text only on a status in the writer's detail set (400,
// 413, and 428 built in, plus whatever [ErrorWriter.Detail] added), where it
// is request-shaped and client-actionable; every other status sends the bare
// title, so an internal error's text never reaches the wire. The returned
// error is the encoder's, as from [WriteProblem].
func (ew *ErrorWriter) Write(w http.ResponseWriter, r *http.Request, err error) error {
	status := ew.Status(err)
	detail := ""
	if _, ok := ew.detail[status]; ok {
		detail = err.Error()
	}
	return WriteProblem(w, r, status, "", detail)
}
