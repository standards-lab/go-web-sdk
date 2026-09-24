package web

import (
	"fmt"
	"net/http"
)

// statusError is the status mapping of this package's own error types: each
// carries its HTTP status beside its definition, and [ErrorWriter.Problem]
// asks the error rather than enumerating the types. The method is
// unexported on purpose. A consumer's status policy is declared through
// matchers at the composition root, never by teaching an error its status,
// so the set of errors the SDK maps itself stays exactly the set it defines.
type statusError interface {
	error
	status() int
}

// QueryError reports one rejected query parameter: which parameter
// ("page", "size", "sort", "cursor", or a filter's key), the offending
// input, and why. [ErrorWriter] maps it to a 400; this package mints no
// problem types.
type QueryError struct {
	Param  string
	Value  string
	Reason string
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("query %s=%q: %s", e.Param, e.Value, e.Reason)
}

func (e *QueryError) status() int { return http.StatusBadRequest }

// PreconditionError reports a version precondition the request failed to
// state or stated unreadably: Missing marks an absent If-Match header, and
// otherwise Value carries the rejected header text. [ErrorWriter] maps it to
// a 428 when Missing and a 400 otherwise.
type PreconditionError struct {
	Missing bool
	Value   string
}

func (e *PreconditionError) Error() string {
	if e.Missing {
		return "the request requires an If-Match header"
	}
	return fmt.Sprintf("If-Match %q: must be one entity-tag containing an integer version, like \"3\"", e.Value)
}

func (e *PreconditionError) status() int {
	if e.Missing {
		return http.StatusPreconditionRequired
	}
	return http.StatusBadRequest
}

// BodyError reports a request body [DecodeJSON] rejected: TooLarge marks a
// body over the caller's limit, and otherwise Reason says what the decoder
// refused — malformed JSON, an unknown field, a second value after the
// first, an empty body. [ErrorWriter] maps it to a 413 when TooLarge and a
// 400 otherwise.
type BodyError struct {
	TooLarge bool
	Reason   string
}

func (e *BodyError) Error() string {
	return "body: " + e.Reason
}

func (e *BodyError) status() int {
	if e.TooLarge {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// UploadError reports a raw request body [ReadUpload] refused before
// reading it. Header names the missing or unparsable header, "Content-Type"
// or "Content-Length"; otherwise TooLarge marks a declared length over the
// caller's limit. [ErrorWriter] maps a missing or unparsable Content-Type
// to a 415, since the service cannot tell what it would be storing, a
// missing Content-Length (a chunked body) to a 411, and TooLarge to a 413.
// A consumer refusing a media type its allowlist does not hold returns one
// with Header "Content-Type", answered 415 the same way.
type UploadError struct {
	Header   string
	TooLarge bool
	Reason   string
}

func (e *UploadError) Error() string {
	return "upload: " + e.Reason
}

func (e *UploadError) status() int {
	switch {
	case e.TooLarge:
		return http.StatusRequestEntityTooLarge
	case e.Header == "Content-Length":
		return http.StatusLengthRequired
	default:
		return http.StatusUnsupportedMediaType
	}
}
