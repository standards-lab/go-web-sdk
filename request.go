package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// IfMatch reads the request's version precondition (RFC 9110 §13.1.1):
// exactly one strong entity-tag whose opaque value is a base-10 integer —
// If-Match: "3". A missing header, a weak tag, the * form, a list, or a
// non-integer tag is a *[PreconditionError]. The parse is syntax only; a
// version no row can hold answers as a failed precondition, not a malformed
// one.
func IfMatch(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return 0, &PreconditionError{Missing: true}
	}
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return 0, &PreconditionError{Value: raw}
	}
	version, err := strconv.ParseInt(raw[1:len(raw)-1], 10, 64)
	if err != nil {
		return 0, &PreconditionError{Value: raw}
	}
	return version, nil
}

// DecodeJSON reads the request body strictly as one JSON value of type T:
// the body is bounded at limit bytes through http.MaxBytesReader (so an
// oversized body also closes the connection, as net/http requires), unknown
// fields are rejected so a misspelled field cannot silently change a
// command's meaning, and anything after the first value is rejected. A body
// that fails any of these, or is empty, is a *[BodyError]; a caller whose
// body is optional checks r.ContentLength first. The decode is syntax and
// shape only — the values' validity is the command's own check.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request, limit int64) (T, error) {
	var v T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, bodyError(err, limit)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after the JSON value")
		}
		return v, bodyError(err, limit)
	}
	return v, nil
}

// bodyError classifies a decoder failure: the reader's overflow is TooLarge,
// an immediate EOF is an empty body, and anything else carries the
// decoder's own reason.
func bodyError(err error, limit int64) *BodyError {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return &BodyError{TooLarge: true, Reason: fmt.Sprintf("exceeds the %d-byte limit", limit)}
	}
	if errors.Is(err, io.EOF) {
		return &BodyError{Reason: "empty body"}
	}
	return &BodyError{Reason: err.Error()}
}
