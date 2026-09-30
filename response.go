package web

import (
	"encoding/json"
	"fmt"
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

// Attachment returns the Content-Disposition value that makes a response a
// download named name, never rendered inline (RFC 6266). The value carries
// the name as a quoted filename, with a quote or a backslash escaped as a
// quoted pair. A name may hold characters outside printable ASCII, or a %,
// which some browsers percent-decode in a plain filename. For such a name
// the quoted filename is a fallback with each such character replaced by an
// underscore, and filename* follows with the name in RFC 8187's encoding,
// which a recipient prefers; a byte that is not valid UTF-8 is carried there
// as U+FFFD. A handler sets the header before calling [WriteObject], which
// keeps it on success.
func Attachment(name string) string {
	var fallback strings.Builder
	plain := true
	for _, c := range name {
		switch {
		case c == '"' || c == '\\':
			fallback.WriteByte('\\')
			fallback.WriteRune(c)
		case c < 0x20 || c > 0x7e || c == '%':
			plain = false
			fallback.WriteByte('_')
		default:
			fallback.WriteRune(c)
		}
	}
	header := `attachment; filename="` + fallback.String() + `"`
	if plain {
		return header
	}
	name = strings.ToValidUTF8(name, "\uFFFD")
	var encoded strings.Builder
	for i := 0; i < len(name); i++ {
		if b := name[i]; attrChar(b) {
			encoded.WriteByte(b)
		} else {
			fmt.Fprintf(&encoded, "%%%02X", b)
		}
	}
	return header + "; filename*=UTF-8''" + encoded.String()
}

// attrChar reports whether b is an RFC 8187 attr-char, the bytes an
// extended value carries unencoded.
func attrChar(b byte) bool {
	switch {
	case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", b) >= 0
}

// WriteObject proxies a stored object's bytes as the response to a GET or
// HEAD, with its validators and X-Content-Type-Options: nosniff. It answers a
// matching If-None-Match or If-Modified-Since with 304 before open is called.
// If-None-Match compares entity tags weakly, with * matching any object; when
// present it outranks If-Modified-Since, which is then ignored (RFC 9110
// §13.2.2). A HEAD sends the headers alone without calling open. WriteObject
// closes what open returns; the caller does not. An error from open is
// returned uncommitted for the error writer to answer, with every
// representation header cleared, the caller's included, and Cache-Control
// set to no-store.
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
			for _, name := range representation {
				h.Del(name)
			}
			// A 404 is heuristically cacheable, so the problem must not be
			// stored in the object's place.
			h.Set("Cache-Control", "no-store")
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

// representation names the headers that describe the object's bytes or
// how long to keep them, set by WriteObject or by its caller for the object,
// which a problem answering a failed open must not carry.
var representation = [...]string{
	"Cache-Control",
	"Content-Disposition",
	"Content-Encoding",
	"Content-Length",
	"Content-Range",
	"ETag",
	"Expires",
	"Last-Modified",
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

// Page is the success envelope of a paginated read, the whole response
// body. Page is the 1-based number of a read addressed by number, omitted
// under a cursor. Total is omitted when the read did not count. Next, when
// present, is the cursor that continues from this page; a read that cannot
// continue by cursor reports More with no Next.
type Page[T any] struct {
	Items []T    `json:"items"`
	Page  int    `json:"page,omitempty"`
	Size  int    `json:"size"`
	Total *int   `json:"total,omitempty"`
	More  bool   `json:"more"`
	Next  string `json:"next,omitempty"`
}

// Paging is what a fulfilled read reports beyond its items: the total row
// count, nil when the read did not count one, whether a further page exists,
// and the cursor that continues from the page. The zero value is an
// uncounted read with no further page.
type Paging struct {
	Total *int
	More  bool
	Next  string
}

// NewPage assembles the envelope from a page's items, the query the read
// honored, and its Paging. Nil items marshal as [].
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
	if p.Total != nil {
		page.Total = new(*p.Total)
	}
	return page
}
