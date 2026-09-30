package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"uuid"
)

// IfMatch reads the request's version precondition (RFC 9110 §13.1.1):
// exactly one strong entity-tag whose opaque value is a base-10 integer,
// If-Match: "3". Anything else is a *[PreconditionError]. The parse is
// syntax only; whether the version matches is the data layer's check.
func IfMatch(r *http.Request) (int64, error) {
	if lines := r.Header.Values("If-Match"); len(lines) > 1 {
		return 0, &PreconditionError{Value: strings.Join(lines, ", ")}
	}
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return 0, &PreconditionError{Missing: true}
	}
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' || !integer(raw[1:len(raw)-1]) {
		return 0, &PreconditionError{Value: raw}
	}
	version, err := strconv.ParseInt(raw[1:len(raw)-1], 10, 64)
	if err != nil {
		return 0, &PreconditionError{Value: raw}
	}
	return version, nil
}

// integer reports whether s is base-10 digits with an optional leading '-':
// ParseInt's syntax without the '+' it also takes.
func integer(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// PathUUID reads the request's {name} path value as a UUID and returns it in
// canonical form, so a store binds one spelling whatever the request sent. A
// value that does not parse is a *[PathError].
func PathUUID(r *http.Request, name string) (string, error) {
	raw := r.PathValue(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return "", &PathError{Name: name, Value: raw}
	}
	return id.String(), nil
}

// DecodeJSON reads the request body as exactly one JSON value of type T,
// bounded at limit bytes and with unknown fields rejected, so a misspelled
// field cannot silently change a command's meaning. A body that fails, or is
// empty, is a *[BodyError]; a caller whose body is optional checks
// r.ContentLength first.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request, limit int64) (T, error) {
	var v T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, bodyError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return v, bodyError(err)
		}
		return v, &BodyError{Reason: "unexpected data after the JSON value"}
	}
	return v, nil
}

// Upload is a raw request body declared by its own headers: the
// Content-Type as sent, for storing and serving back; its media type
// lower-cased without parameters, for a consumer's allowlist; the declared
// size; and the body, bounded at the caller's limit.
type Upload struct {
	ContentType string
	MediaType   string
	Size        int64
	Body        io.Reader
}

// ReadUpload accepts a raw body, such as a file's bytes, by its headers
// before any byte is read: a type/subtype Content-Type and a declared
// Content-Length within limit, so a store that needs the size up front takes
// the stream without buffering it. A refusal is an *[UploadError]; a request
// with no body at all is a 0-byte upload.
func ReadUpload(w http.ResponseWriter, r *http.Request, limit int64) (Upload, error) {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return Upload{}, &UploadError{Header: "Content-Type", Reason: "the request requires a Content-Type header"}
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err == nil && !strings.Contains(mediaType, "/") {
		err = errors.New("not a type/subtype media type")
	}
	if err != nil {
		return Upload{}, &UploadError{Header: "Content-Type", Reason: fmt.Sprintf("Content-Type %q: %v", ct, err)}
	}
	if r.ContentLength < 0 {
		return Upload{}, &UploadError{Header: "Content-Length", Reason: "the request requires a Content-Length header, not a chunked body"}
	}
	if r.ContentLength > limit {
		return Upload{}, &UploadError{
			TooLarge: true,
			Reason:   fmt.Sprintf("the declared %d bytes exceed the %d-byte limit", r.ContentLength, limit),
		}
	}
	return Upload{
		ContentType: ct,
		MediaType:   mediaType,
		Size:        r.ContentLength,
		Body:        http.MaxBytesReader(w, r.Body, limit),
	}, nil
}

// bodyError classifies a decoder failure; an overflow names the limit the
// read hit, the tighter of DecodeJSON's and an outer middleware.BodyLimit's.
func bodyError(err error) *BodyError {
	if mbe, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return &BodyError{TooLarge: true, Reason: fmt.Sprintf("exceeds the %d-byte limit", mbe.Limit)}
	}
	if errors.Is(err, io.EOF) {
		return &BodyError{Reason: "empty body"}
	}
	return &BodyError{Reason: err.Error()}
}
