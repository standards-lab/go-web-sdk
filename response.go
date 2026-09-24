package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// JSONMediaType is the media type [WriteJSON] sets.
const JSONMediaType = "application/json"

// WriteJSON sends data as JSON with the given status.
func WriteJSON(
	w http.ResponseWriter,
	status int,
	data any,
) error {
	w.Header().Set("Content-Type", JSONMediaType)
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

// Object describes a stored object a response proxies: its media type, its
// size in bytes, its entity tag as an HTTP ETag header carries it (quoted,
// optionally weak), and when it last changed. An empty ContentType is sent
// as application/octet-stream, a negative Size omits Content-Length, and an
// empty ETag or a zero ModifiedAt omits its header. Size must be the length
// of the bytes the object's opener returns, since net/http holds the
// response to the length it declared.
type Object struct {
	ContentType string
	Size        int64
	ETag        string
	ModifiedAt  time.Time
}

// WriteObject proxies a stored object's bytes as the response to a GET or
// HEAD request: it sets Content-Type, Content-Length, ETag, and
// Last-Modified from o, and X-Content-Type-Options: nosniff, since the
// bytes are content the service stored rather than wrote, and a browser
// must not reinterpret them as another type.
//
// The request's preconditions are answered from o alone, before any byte
// is opened (RFC 9110 §13.2.2): an If-None-Match naming o's entity tag by
// weak comparison, or *, answers 304 Not Modified, and when If-None-Match
// is absent, an If-Modified-Since no earlier than ModifiedAt does too. A
// HEAD request answers with the headers alone. open is called only when
// the bytes are sent, so a revalidation or a HEAD costs the store nothing
// beyond the metadata o was built from; WriteObject closes what it
// returns. An error from open is returned before anything is committed,
// with the validators cleared, so an adapted handler writes it as a
// problem. Headers the caller set before the call, such as
// Content-Disposition or Cache-Control, are kept. An error from the copy
// is returned after the response is committed, so an adapted handler logs
// it rather than writing a second response.
func WriteObject(w http.ResponseWriter, r *http.Request, o Object, open func() (io.ReadCloser, error)) error {
	h := w.Header()
	if o.ETag != "" {
		h.Set("ETag", o.ETag)
	}
	if !o.ModifiedAt.IsZero() {
		h.Set("Last-Modified", o.ModifiedAt.UTC().Format(http.TimeFormat))
	}
	if notModified(r, o) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	var body io.ReadCloser
	if r.Method != http.MethodHead {
		var err error
		if body, err = open(); err != nil {
			h.Del("ETag")
			h.Del("Last-Modified")
			return err
		}
		defer func() { _ = body.Close() }()
	}

	contentType := o.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	h.Set("Content-Type", contentType)
	if o.Size >= 0 {
		h.Set("Content-Length", strconv.FormatInt(o.Size, 10))
	}
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if body == nil {
		return nil
	}
	_, err := io.Copy(w, body)
	return err
}

// notModified evaluates a GET or HEAD request's cache preconditions against
// o, in RFC 9110 §13.2.2's order: If-None-Match when present, and
// If-Modified-Since only in its absence. A date that does not parse is
// ignored, as the RFC requires, and so is either header on another method.
func notModified(r *http.Request, o Object) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		return noneMatch(inm, o.ETag)
	}
	ims := r.Header.Get("If-Modified-Since")
	if ims == "" || o.ModifiedAt.IsZero() {
		return false
	}
	since, err := http.ParseTime(ims)
	if err != nil {
		return false
	}
	return !o.ModifiedAt.Truncate(time.Second).After(since)
}

// noneMatch reports whether an If-None-Match header value matches etag by
// weak comparison: the * form matches any current object, and otherwise
// one of the listed entity tags must equal etag once a W/ prefix is set
// aside on either side. The list is scanned tag by tag, since a quoted tag
// may itself contain a comma; a malformed remainder ends the scan.
func noneMatch(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "*" {
		return true
	}
	if etag == "" {
		return false
	}
	want := strings.TrimPrefix(etag, "W/")
	for header != "" {
		header = strings.TrimLeft(header, " \t,")
		header = strings.TrimPrefix(header, "W/")
		if !strings.HasPrefix(header, `"`) {
			return false
		}
		end := strings.IndexByte(header[1:], '"')
		if end < 0 {
			return false
		}
		if header[:end+2] == want {
			return true
		}
		header = header[end+2:]
	}
	return false
}

// NoTotal is the [Paging] total of a read that did not count its rows:
// [NewPage] omits the envelope's total for it, where a total of 0 is a
// counted, empty collection.
const NoTotal = -1

// Page is the success envelope of a paginated read: one page of items, the
// address that produced it, the total row count across all pages when the
// read counted one, whether a further page exists, and the cursor that
// continues from this page. It is the whole response body, written with
// [WriteJSON].
//
// Page is the 1-based number of a read addressed by number and is omitted
// for a read addressed by cursor, which has no number. Total is omitted
// when the read did not count, so a client never mistakes an uncounted
// read for an empty collection. More reports whether a further page exists,
// and Next, when present, is the token a client sends as the cursor
// parameter to read it; a read whose ordering cannot be continued by cursor
// reports More with no Next, and the client pages by number instead.
type Page[T any] struct {
	Items []T    `json:"items"`
	Page  int    `json:"page,omitempty"`
	Size  int    `json:"size"`
	Total *int   `json:"total,omitempty"`
	More  bool   `json:"more"`
	Next  string `json:"next,omitempty"`
}

// Paging is what a fulfilled read reports beyond its items: the total row
// count, [NoTotal] (or any negative) when the read did not count one,
// whether a further page exists, and the cursor that continues from the
// page, empty when there is none. A data layer's own collection type maps
// onto it directly, converting its cursor type to a string. The zero value
// reports a counted, empty read, so a read that did not count says so.
type Paging struct {
	Total int
	More  bool
	Next  string
}

// NewPage assembles the envelope from a fulfilled read: the page's items,
// the query the read honored, and what the read reported. Nil items become
// an empty slice, so an empty page marshals its items as [] rather than
// null, and a negative total becomes an absent one.
func NewPage[T any](items []T, q Query, p Paging) Page[T] {
	if items == nil {
		items = []T{}
	}
	page := Page[T]{
		Items: items,
		Page:  q.Page,
		Size:  q.Size,
		More:  p.More,
		Next:  p.Next,
	}
	if p.Total >= 0 {
		total := p.Total
		page.Total = &total
	}
	return page
}
