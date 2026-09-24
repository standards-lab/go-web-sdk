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
		return v, bodyError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after the JSON value")
		}
		return v, bodyError(err)
	}
	return v, nil
}

// Upload is a raw request body declared by its own headers: the media type
// the client named, the exact size it declared, and the body, which reads
// at most Size bytes.
type Upload struct {
	ContentType string
	Size        int64
	Body        io.Reader
}

// ReadUpload accepts a raw request body, such as a file's bytes, by its
// headers alone, before any byte is read: the request must name a
// Content-Type that parses as a media type, and must declare a
// Content-Length, so the size is known before the body is stored anywhere
// and a store that needs it up front never buffers the stream to learn it.
// A missing or unparsable type, a missing length (a chunked body), or a
// declared length over limit is a *[UploadError]. The body is bounded at
// limit through http.MaxBytesReader, as [DecodeJSON]'s is, and net/http
// itself holds the body to its declared length. ReadUpload checks headers
// only: whether the service stores that media type is the consumer's check.
func ReadUpload(w http.ResponseWriter, r *http.Request, limit int64) (Upload, error) {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return Upload{}, &UploadError{Header: "Content-Type", Reason: "the request requires a Content-Type header"}
	}
	if _, _, err := mime.ParseMediaType(ct); err != nil {
		return Upload{}, &UploadError{Header: "Content-Type", Reason: fmt.Sprintf("Content-Type %q: %v", ct, err)}
	}
	if r.ContentLength < 0 {
		return Upload{}, &UploadError{Header: "Content-Length", Reason: "the request requires a Content-Length header"}
	}
	if r.ContentLength > limit {
		return Upload{}, &UploadError{
			TooLarge: true,
			Reason:   fmt.Sprintf("the declared %d bytes exceed the %d-byte limit", r.ContentLength, limit),
		}
	}
	return Upload{
		ContentType: ct,
		Size:        r.ContentLength,
		Body:        http.MaxBytesReader(w, r.Body, limit),
	}, nil
}

// bodyError classifies a decoder failure: the reader's overflow is TooLarge,
// named by the limit the read actually hit — the reader's own, from
// [http.MaxBytesError], which is the tighter of DecodeJSON's limit and an
// outer [middleware.BodyLimit]'s when both wrap the body — an immediate EOF
// is an empty body, and anything else carries the decoder's own reason.
func bodyError(err error) *BodyError {
	if mbe, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return &BodyError{TooLarge: true, Reason: fmt.Sprintf("exceeds the %d-byte limit", mbe.Limit)}
	}
	if errors.Is(err, io.EOF) {
		return &BodyError{Reason: "empty body"}
	}
	return &BodyError{Reason: err.Error()}
}
