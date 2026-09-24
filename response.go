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
// optionally weak), and when it last changed. An empty ETag or a zero
// ModifiedAt is omitted from the response.
type Object struct {
	ContentType string
	Size        int64
	ETag        string
	ModifiedAt  time.Time
}

// WriteObject proxies a stored object's bytes as the response: it sets
// Content-Type, Content-Length, ETag, and Last-Modified from o, and
// X-Content-Type-Options: nosniff, since the bytes are content the service
// stored rather than wrote, and a browser must not reinterpret them as
// another type. A request whose If-None-Match names o's entity tag (by weak
// comparison, RFC 9110 §13.1.2) or is * answers 304 Not Modified with no
// body, and a HEAD request answers with the headers alone. Headers the caller set before the call, such as
// Content-Disposition or Cache-Control, are kept. body is read to its end
// and is the caller's to close. The returned error is the copy's, after the
// response is committed, so an adapted handler logs it rather than writing
// a second response.
func WriteObject(w http.ResponseWriter, r *http.Request, o Object, body io.Reader) error {
	h := w.Header()
	if o.ETag != "" {
		h.Set("ETag", o.ETag)
	}
	if !o.ModifiedAt.IsZero() {
		h.Set("Last-Modified", o.ModifiedAt.UTC().Format(http.TimeFormat))
	}
	if noneMatch(r.Header.Get("If-None-Match"), o.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	h.Set("Content-Type", o.ContentType)
	h.Set("Content-Length", strconv.FormatInt(o.Size, 10))
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return nil
	}
	_, err := io.Copy(w, body)
	return err
}

// noneMatch reports whether an If-None-Match header value matches etag by
// weak comparison: the * form matches any current object, and otherwise
// one of the listed tags must equal etag once a W/ prefix is set aside on
// either side.
func noneMatch(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	if etag == "" {
		return false
	}
	want := strings.TrimPrefix(etag, "W/")
	for tag := range strings.SplitSeq(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(tag), "W/") == want {
			return true
		}
	}
	return false
}

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
// count, negative when the read did not count one, whether a further page
// exists, and the cursor that continues from the page, empty when there is
// none. A data layer's own collection type maps onto it field for field.
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
