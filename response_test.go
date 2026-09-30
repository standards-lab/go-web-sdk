package web_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

func TestWriteJSON_SetsMediaTypeAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()

	err := web.WriteJSON(rec, http.StatusCreated, map[string]string{"id": "42"})
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != web.JSONMediaType {
		t.Errorf("Content-Type = %q, want %q", got, web.JSONMediaType)
	}
	if got := decodeBody(t, rec)["id"]; got != "42" {
		t.Errorf("id = %v, want 42", got)
	}
}

func TestNewPage_CarriesTheQuery(t *testing.T) {
	d := web.Query{Page: 2, Size: 25}

	p := web.NewPage([]string{"a", "b"}, d, web.Paging{Total: new(51), More: true, Next: "c2"})

	total := 51
	want := web.Page[string]{Items: []string{"a", "b"}, Page: 2, Size: 25, Total: &total, More: true, Next: "c2"}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("page = %+v, want %+v", p, want)
	}
}

func TestNewPage_NilItemsMarshalAsEmptyArray(t *testing.T) {
	p := web.NewPage[string](nil, web.Query{Page: 1, Size: 25}, web.Paging{Total: new(0)})

	body, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"items":[],"page":1,"size":25,"total":0,"more":false}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPage_WireShape(t *testing.T) {
	type row struct {
		Name string `json:"name"`
	}

	body, err := json.Marshal(web.NewPage([]row{{Name: "ops"}}, web.Query{Page: 3, Size: 10}, web.Paging{Total: new(21), More: true}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"items":[{"name":"ops"}],"page":3,"size":10,"total":21,"more":true}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPage_CursorPageOmitsTheNumberAndAnUncountedTotal(t *testing.T) {
	q := web.Query{Size: 10, Cursor: "c1"}

	body, err := json.Marshal(web.NewPage([]string{"x"}, q, web.Paging{More: true, Next: "c2"}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"items":["x"],"size":10,"more":true,"next":"c2"}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPage_ZeroTotalIsCountedNotAbsent(t *testing.T) {
	p := web.NewPage[string](nil, web.Query{Page: 1, Size: 10}, web.Paging{Total: new(0)})

	if p.Total == nil || *p.Total != 0 {
		t.Errorf("total = %v, want a counted 0", p.Total)
	}
}

func objectRequest(method string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, "/files/1/content", nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return r
}

var logo = web.Object{
	ContentType: "image/png",
	Size:        9,
	ETag:        `"0x8DC"`,
	ModifiedAt:  time.Date(2026, 9, 24, 12, 0, 0, 0, time.FixedZone("EDT", -4*3600)),
}

// opener counts its calls and records whether what it opened was closed.
type opener struct {
	body   string
	err    error
	calls  int
	closed bool
}

func (o *opener) open() (io.ReadCloser, error) {
	o.calls++
	if o.err != nil {
		return nil, o.err
	}
	return readCloser{strings.NewReader(o.body), &o.closed}, nil
}

type readCloser struct {
	io.Reader
	closed *bool
}

func (rc readCloser) Close() error { *rc.closed = true; return nil }

func TestWriteObject_ProxiesTheBytesWithTheirHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Disposition", `inline; filename="logo.png"`)
	o := &opener{body: "png-bytes"}

	if err := web.WriteObject(rec, objectRequest(http.MethodGet), logo, o.open); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	want := map[string]string{
		"Content-Type":           "image/png",
		"Content-Length":         "9",
		"ETag":                   `"0x8DC"`,
		"Last-Modified":          "Thu, 24 Sep 2026 16:00:00 GMT",
		"X-Content-Type-Options": "nosniff",
		"Content-Disposition":    `inline; filename="logo.png"`,
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	if rec.Body.String() != "png-bytes" {
		t.Errorf("body = %q, want png-bytes", rec.Body.String())
	}
	if !o.closed {
		t.Error("the opened body was not closed")
	}
}

func TestWriteObject_MatchingIfNoneMatchIs304WithoutOpening(t *testing.T) {
	for _, header := range []string{`"0x8DC"`, `W/"0x8DC"`, `"other", "0x8DC"`, `W/"a", W/"0x8DC"`, "*"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+" "+header, func(t *testing.T) {
				rec := httptest.NewRecorder()
				o := &opener{body: "png-bytes"}

				if err := web.WriteObject(rec, objectRequest(method, "If-None-Match", header), logo, o.open); err != nil {
					t.Fatalf("WriteObject: %v", err)
				}

				if rec.Code != http.StatusNotModified {
					t.Errorf("status = %d, want 304", rec.Code)
				}
				if rec.Body.Len() != 0 || o.calls != 0 {
					t.Errorf("body = %q, opens = %d, want none", rec.Body.String(), o.calls)
				}
				if got := rec.Header().Get("ETag"); got != `"0x8DC"` {
					t.Errorf("ETag = %q, want the object's", got)
				}
			})
		}
	}
}

func TestWriteObject_QuotedTagMayHoldAComma(t *testing.T) {
	rec := httptest.NewRecorder()
	o := &opener{body: "x"}
	obj := web.Object{ContentType: "text/plain", Size: 1, ETag: `"a,b"`}

	if err := web.WriteObject(rec, objectRequest(http.MethodGet, "If-None-Match", `"z", "a,b"`), obj, o.open); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	if rec.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", rec.Code)
	}
}

func TestWriteObject_NonMatchingIfNoneMatchSendsTheBytes(t *testing.T) {
	for _, header := range []string{`"0x1"`, `"0x8DC`, `garbage`} {
		t.Run(header, func(t *testing.T) {
			rec := httptest.NewRecorder()
			o := &opener{body: "png-bytes"}

			if err := web.WriteObject(rec, objectRequest(http.MethodGet, "If-None-Match", header), logo, o.open); err != nil {
				t.Fatalf("WriteObject: %v", err)
			}

			if rec.Code != http.StatusOK || rec.Body.String() != "png-bytes" {
				t.Errorf("status, body = %d, %q, want 200, png-bytes", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWriteObject_IfModifiedSince(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		status  int
	}{
		{"at the modification time", []string{"If-Modified-Since", "Thu, 24 Sep 2026 16:00:00 GMT"}, http.StatusNotModified},
		{"after it", []string{"If-Modified-Since", "Fri, 25 Sep 2026 16:00:00 GMT"}, http.StatusNotModified},
		{"before it", []string{"If-Modified-Since", "Wed, 23 Sep 2026 16:00:00 GMT"}, http.StatusOK},
		{"unparsable", []string{"If-Modified-Since", "yesterday"}, http.StatusOK},
		{"ignored beside If-None-Match", []string{"If-Modified-Since", "Fri, 25 Sep 2026 16:00:00 GMT", "If-None-Match", `"0x1"`}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			o := &opener{body: "png-bytes"}

			if err := web.WriteObject(rec, objectRequest(http.MethodGet, tt.headers...), logo, o.open); err != nil {
				t.Fatalf("WriteObject: %v", err)
			}

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

func TestWriteObject_HeadSendsHeadersWithoutOpening(t *testing.T) {
	rec := httptest.NewRecorder()
	o := &opener{body: "png-bytes"}

	if err := web.WriteObject(rec, objectRequest(http.MethodHead), logo, o.open); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || o.calls != 0 {
		t.Errorf("status, body, opens = %d, %q, %d, want 200, none, 0", rec.Code, rec.Body.String(), o.calls)
	}
	if got := rec.Header().Get("Content-Length"); got != "9" {
		t.Errorf("Content-Length = %q, want 9", got)
	}
}

func TestWriteObject_OpenErrorIsReturnedUncommitted(t *testing.T) {
	missing := errors.New("object not found")
	rec := httptest.NewRecorder()
	o := &opener{err: missing}
	// The representation headers a handler sets for the object before the
	// call describe bytes the problem is not: a browser would save a JSON
	// 404 under the object's name and cache it for the object's lifetime.
	rec.Header().Set("Content-Disposition", web.Attachment("logo.png"))
	rec.Header().Set("Cache-Control", "private, max-age=3600")
	rec.Header().Set("Content-Encoding", "gzip")
	rec.Header().Set("Content-Length", "9")
	rec.Header().Set("Content-Range", "bytes 0-8/9")
	rec.Header().Set("Expires", "Thu, 01 Oct 2026 00:00:00 GMT")

	err := web.WriteObject(rec, objectRequest(http.MethodGet), logo, o.open)

	if !errors.Is(err, missing) {
		t.Fatalf("error = %v, want the opener's", err)
	}
	representation := []string{"ETag", "Last-Modified", "Content-Type", "Content-Length", "Content-Disposition", "Content-Encoding", "Content-Range", "Expires"}
	for _, h := range representation {
		if got := rec.Header().Get(h); got != "" {
			t.Errorf("%s = %q, want none on an uncommitted error", h, got)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store on an uncommitted error", got)
	}
	ew := web.NewErrorWriter(discard, matcherFor(missing, http.StatusNotFound))
	if werr := ew.Write(rec, objectRequest(http.MethodGet), err); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want the problem's 404", rec.Code)
	}
	for _, h := range representation[3:] {
		if got := rec.Result().Header.Get(h); got != "" {
			t.Errorf("problem response %s = %q, want none", h, got)
		}
	}
	if got := rec.Result().Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("problem response Cache-Control = %q, want no-store", got)
	}
}

func TestWriteObject_CopyErrorIsReturnedAfterCommit(t *testing.T) {
	broken := errors.New("stream reset")
	rec := httptest.NewRecorder()
	open := func() (io.ReadCloser, error) {
		return io.NopCloser(io.MultiReader(strings.NewReader("png"), iotestErrReader{broken})), nil
	}

	err := web.WriteObject(rec, objectRequest(http.MethodGet), logo, open)

	if !errors.Is(err, broken) {
		t.Errorf("error = %v, want the copy's", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the committed 200", rec.Code)
	}
}

type iotestErrReader struct{ err error }

func (r iotestErrReader) Read([]byte) (int, error) { return 0, r.err }

func TestWriteObject_Defaults(t *testing.T) {
	rec := httptest.NewRecorder()
	o := &opener{body: "hi"}

	err := web.WriteObject(rec, objectRequest(http.MethodGet, "If-None-Match", `"x"`), web.Object{Size: -1}, o.open)
	if err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
	for _, h := range []string{"ETag", "Last-Modified", "Content-Length"} {
		if got := rec.Header().Get(h); got != "" {
			t.Errorf("%s = %q, want none", h, got)
		}
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "hi" {
		t.Errorf("status, body = %d, %q, want 200, hi", rec.Code, rec.Body.String())
	}
}

// A read that did not set Total emits no total: the zero Paging is an
// uncounted read, never a counted, empty one.
func TestPaging_ZeroValueIsUncounted(t *testing.T) {
	p := web.NewPage([]string{"a"}, web.Query{Page: 1, Size: 1}, web.Paging{})

	if p.Total != nil {
		t.Errorf("total = %d, want absent", *p.Total)
	}
}

// The envelope's total is its own: a later change to the read's count does
// not reach a page already assembled.
func TestNewPage_CopiesTheTotal(t *testing.T) {
	total := 3
	p := web.NewPage([]string{"a"}, web.Query{Page: 1, Size: 1}, web.Paging{Total: &total})
	total = 4

	if *p.Total != 3 {
		t.Errorf("total = %d, want the 3 the read reported", *p.Total)
	}
}

func TestAttachment(t *testing.T) {
	cases := map[string]struct{ name, want string }{
		"plain":     {"report.txt", `attachment; filename="report.txt"`},
		"quoted":    {`a "b"\c.html`, `attachment; filename="a \"b\"\\c.html"`},
		"control":   {"a\tb.txt", `attachment; filename="a_b.txt"; filename*=UTF-8''a%09b.txt`},
		"crlf":      {"a\r\nSet-Cookie: x.txt", `attachment; filename="a__Set-Cookie: x.txt"; filename*=UTF-8''a%0D%0ASet-Cookie%3A%20x.txt`},
		"del":       {"a\x7fb", `attachment; filename="a_b"; filename*=UTF-8''a%7Fb`},
		"percent":   {"100%41.txt", `attachment; filename="100_41.txt"; filename*=UTF-8''100%2541.txt`},
		"encoded":   {"it's*.txt", `attachment; filename="it's*.txt"`},
		"invalid":   {"\xff.txt", `attachment; filename="_.txt"; filename*=UTF-8''%EF%BF%BD.txt`},
		"empty":     {"", `attachment; filename=""`},
		"non-ascii": {"résumé 1.pdf", `attachment; filename="r_sum_ 1.pdf"; filename*=UTF-8''r%C3%A9sum%C3%A9%201.pdf`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := web.Attachment(c.name); got != c.want {
				t.Errorf("Attachment(%q) = %s, want %s", c.name, got, c.want)
			}
		})
	}
}
