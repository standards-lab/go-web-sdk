package web

import (
	"fmt"
	"net/http"
)

// statusError is implemented by this package's own errors, each carrying its
// status. The method is unexported so a consumer's status policy stays in
// its matchers.
type statusError interface {
	error
	status() int
}

// QueryError reports one rejected query parameter: its name, the offending
// input, and why. [ErrorWriter] maps it to a 400.
type QueryError struct {
	Param  string
	Value  string
	Reason string
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("query %s=%q: %s", e.Param, e.Value, e.Reason)
}

func (e *QueryError) status() int { return http.StatusBadRequest }

// PreconditionError reports an If-Match header [IfMatch] rejected: Missing
// marks an absent header, and otherwise Value carries its text.
// [ErrorWriter] maps it to a 428 when Missing and a 400 otherwise.
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
// body over the limit, and otherwise Reason says what the decoder refused.
// [ErrorWriter] maps it to a 413 when TooLarge and a 400 otherwise.
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

// UploadError reports a raw request body refused before it was read: Header
// names a missing or unacceptable "Content-Type" or a missing
// "Content-Length", and otherwise TooLarge marks a declared length over the
// limit. [ErrorWriter] maps it to a 415, 411, or 413 respectively. A
// consumer refusing a media type returns one with Header "Content-Type".
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

// PathError reports a path value [PathUUID] rejected as a malformed UUID:
// Name is the path wildcard and Value the text the request carried.
// [ErrorWriter] maps it to a 400.
type PathError struct {
	Name  string
	Value string
}

func (e *PathError) Error() string {
	return fmt.Sprintf("path %s=%q: must be a UUID", e.Name, e.Value)
}

func (e *PathError) status() int { return http.StatusBadRequest }
