package upload_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/upload"
)

const (
	chunkSize = 16
	prefix    = "/api/uploads"

	mismatchingOffset  = "https://iana.org/assignments/http-problem-types#mismatching-upload-offset"
	inconsistentLength = "https://iana.org/assignments/http-problem-types#inconsistent-upload-length"
)

var fileSum = sum([]byte("the whole file"))

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func digestField(t *testing.T, b []byte) string {
	t.Helper()
	f, err := upload.FormatDigest(sum(b))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

type completions struct {
	mu    sync.Mutex
	files []upload.File
}

func (c *completions) complete(w http.ResponseWriter, _ *http.Request, f upload.File) error {
	c.mu.Lock()
	c.files = append(c.files, f)
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	return nil
}

func (c *completions) all() []upload.File {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]upload.File(nil), c.files...)
}

func handlerOver(store upload.Store, done *completions) http.Handler {
	h := upload.Handler{
		Store:    store,
		Transfer: web.NewTransfer(chunkSize, 1<<20, time.Second),
		Lifetime: time.Hour,
		Complete: done.complete,
		Logger:   slog.New(slog.DiscardHandler),
	}
	api := web.NewGroup("/api")
	api.Mount(h.Routes("/uploads"))
	return web.NewModule(api)
}

type fixture struct {
	t       *testing.T
	store   *upload.MemoryStore
	handler http.Handler
	done    *completions
}

func newFixture(t *testing.T) *fixture {
	store := upload.NewMemoryStore()
	done := &completions{}
	return &fixture{t: t, store: store, handler: handlerOver(store, done), done: done}
}

func (f *fixture) serve(r *http.Request) *http.Response {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	return rec.Result()
}

func creation(complete string, body []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, prefix, bytes.NewReader(body))
	r.Header.Set("Upload-Complete", complete)
	r.Header.Set("Content-Type", "application/pdf")
	r.Header.Set("Content-Disposition", `attachment; filename="report.pdf"`)
	digest, _ := upload.FormatDigest(fileSum)
	r.Header.Set("Repr-Digest", digest)
	return r
}

func (f *fixture) create(length int64) string {
	f.t.Helper()
	r := creation("?0", nil)
	if length >= 0 {
		r.Header.Set("Upload-Length", strconv.FormatInt(length, 10))
	}
	res := f.serve(r)
	if res.StatusCode != http.StatusCreated {
		f.t.Fatalf("creation: status %d, want 201", res.StatusCode)
	}
	return res.Header.Get("Location")
}

func appendRequest(t *testing.T, location string, offset int64, complete string, chunk []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPatch, location, bytes.NewReader(chunk))
	r.Header.Set("Content-Type", "application/partial-upload")
	r.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
	r.Header.Set("Upload-Complete", complete)
	r.Header.Set("Content-Digest", digestField(t, chunk))
	return r
}

func id(location string) string {
	return strings.TrimPrefix(location, prefix+"/")
}

func (f *fixture) offset(location string) int64 {
	f.t.Helper()
	offset, _, _, _, err := f.store.Progress(f.t.Context(), id(location))
	if err != nil {
		f.t.Fatalf("Progress: %v", err)
	}
	return offset
}

func problem(t *testing.T, res *http.Response) web.Problem {
	t.Helper()
	var p web.Problem
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatalf("decoding the problem: %v", err)
	}
	return p
}

func limits(t *testing.T, res *http.Response) (appendSize, age int64) {
	t.Helper()
	d, ok := upload.ParseDictionary(res.Header.Get("Upload-Limit"))
	if !ok {
		t.Fatalf("Upload-Limit %q does not parse", res.Header.Get("Upload-Limit"))
	}
	for _, m := range d {
		switch m.Key {
		case "max-append-size":
			appendSize = m.Value.(int64)
		case "max-age":
			age = m.Value.(int64)
		}
	}
	return appendSize, age
}

func wantLimits(t *testing.T, res *http.Response) {
	t.Helper()
	size, age := limits(t, res)
	if size != chunkSize {
		t.Errorf("max-append-size = %d, want %d", size, chunkSize)
	}
	if age <= 3500 || age > 3600 {
		t.Errorf("max-age = %d, want within the hour's last seconds", age)
	}
}

func wantIncomplete(t *testing.T, res *http.Response) {
	t.Helper()
	if got := res.Header.Get("Upload-Complete"); got != "?0" {
		t.Errorf("Upload-Complete = %q, want ?0", got)
	}
}

func TestCreate(t *testing.T) {
	f := newFixture(t)
	r := creation("?0", nil)
	r.Header.Set("Upload-Length", "40")
	res := f.serve(r)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201", res.StatusCode)
	}
	wantIncomplete(t, res)
	wantLimits(t, res)
	loc := res.Header.Get("Location")
	if !strings.HasPrefix(loc, prefix+"/") {
		t.Fatalf("Location = %q, want it under %s", loc, prefix)
	}
	u, err := uuid.Parse(id(loc))
	if err != nil || u.String()[14] != '4' {
		t.Errorf("Location's id %q is not a UUIDv4", id(loc))
	}
	if got := res.Header.Get("Upload-Draft-Interop-Version"); got != "" {
		t.Errorf("Upload-Draft-Interop-Version = %q on the final response", got)
	}
	mediaType, digest, name, err := f.store.Meta(t.Context(), id(loc))
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "application/pdf" || digest != fileSum || name != "report.pdf" {
		t.Errorf("kept %q, %q, %q; want the creation's type, digest, and name", mediaType, digest, name)
	}
	if _, length, _, _, _ := f.store.Progress(t.Context(), id(loc)); length != 40 {
		t.Errorf("length = %d, want 40", length)
	}
	if again := f.create(-1); again == loc {
		t.Errorf("two creations share Location %q", loc)
	}
}

func TestCreateInterim(t *testing.T) {
	srv := httptest.NewServer(handlerOver(upload.NewMemoryStore(), &completions{}))
	defer srv.Close()

	var mu sync.Mutex
	var interims []textproto.MIMEHeader
	trace := &httptrace.ClientTrace{
		Got1xxResponse: func(code int, h textproto.MIMEHeader) error {
			if code == 104 {
				mu.Lock()
				interims = append(interims, h)
				mu.Unlock()
			}
			return nil
		},
	}
	chunk := []byte("first")
	r, err := http.NewRequestWithContext(httptrace.WithClientTrace(t.Context(), trace), http.MethodPost, srv.URL+prefix, bytes.NewReader(chunk))
	if err != nil {
		t.Fatal(err)
	}
	r.Header = creation("?0", nil).Header.Clone()
	r.Header.Set("Upload-Draft-Interop-Version", "11")
	r.Header.Set("Content-Digest", digestField(t, chunk))
	res, err := srv.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	mu.Lock()
	defer mu.Unlock()
	if len(interims) != 1 {
		t.Fatalf("got %d 104 responses, want 1", len(interims))
	}
	interim := interims[0]
	if interim.Get("Upload-Draft-Interop-Version") != "11" {
		t.Errorf("104 Upload-Draft-Interop-Version = %q, want 11", interim.Get("Upload-Draft-Interop-Version"))
	}
	if interim.Get("Location") == "" || interim.Get("Location") != res.Header.Get("Location") {
		t.Errorf("104 Location %q, final Location %q: want one URI", interim.Get("Location"), res.Header.Get("Location"))
	}
	if interim.Get("Upload-Limit") == "" {
		t.Error("104 carries no Upload-Limit")
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("final status %d, want 201", res.StatusCode)
	}
	if got := res.Header.Get("Upload-Draft-Interop-Version"); got != "" {
		t.Errorf("final response repeats Upload-Draft-Interop-Version %q", got)
	}
	if got := res.Header.Get("Upload-Offset"); got != "5" {
		t.Errorf("Upload-Offset = %q, want 5", got)
	}
	wantIncomplete(t, res)
	wantLimits(t, res)
}

func TestCreateWithoutInterop(t *testing.T) {
	srv := httptest.NewServer(handlerOver(upload.NewMemoryStore(), &completions{}))
	defer srv.Close()
	for _, version := range []string{"", "10"} {
		got := 0
		trace := &httptrace.ClientTrace{
			Got1xxResponse: func(int, textproto.MIMEHeader) error { got++; return nil },
		}
		r, err := http.NewRequestWithContext(httptrace.WithClientTrace(t.Context(), trace), http.MethodPost, srv.URL+prefix, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header = creation("?0", nil).Header.Clone()
		if version != "" {
			r.Header.Set("Upload-Draft-Interop-Version", version)
		}
		res, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if got != 0 || res.StatusCode != http.StatusCreated {
			t.Errorf("interop %q: %d interim responses and status %d, want none and 201", version, got, res.StatusCode)
		}
	}
}

func TestCreateContent(t *testing.T) {
	chunk := []byte("first part")
	cases := []struct {
		name       string
		digest     string
		wantStatus int
		wantOffset int64
	}{
		{"covered", digestField(t, chunk), http.StatusCreated, int64(len(chunk))},
		{"uncovered", "", http.StatusCreated, 0},
		{"mismatched", digestField(t, []byte("other")), http.StatusBadRequest, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			r := creation("?0", chunk)
			r.ContentLength = -1
			if c.digest != "" {
				r.Header.Set("Content-Digest", c.digest)
			}
			res := f.serve(r)
			if res.StatusCode != c.wantStatus {
				t.Fatalf("status %d, want %d", res.StatusCode, c.wantStatus)
			}
			wantIncomplete(t, res)
			if got := f.offset(res.Header.Get("Location")); got != c.wantOffset {
				t.Errorf("offset = %d, want %d", got, c.wantOffset)
			}
		})
	}
}

func TestCreateRefusals(t *testing.T) {
	cases := []struct {
		name   string
		edit   func(*http.Request)
		status int
		typ    string
	}{
		{"no Upload-Complete", func(r *http.Request) { r.Header.Del("Upload-Complete") }, http.StatusBadRequest, ""},
		{"invalid Upload-Complete", func(r *http.Request) { r.Header.Set("Upload-Complete", "yes") }, http.StatusBadRequest, ""},
		{"no Repr-Digest", func(r *http.Request) { r.Header.Del("Repr-Digest") }, http.StatusBadRequest, ""},
		{"no Content-Type", func(r *http.Request) { r.Header.Del("Content-Type") }, http.StatusUnsupportedMediaType, ""},
		{"Content-Encoding", func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, http.StatusUnsupportedMediaType, ""},
		{"declared too large", func(r *http.Request) {
			r.Body = io.NopCloser(bytes.NewReader(make([]byte, chunkSize+1)))
			r.ContentLength = chunkSize + 1
		}, http.StatusRequestEntityTooLarge, ""},
		{"too large", func(r *http.Request) {
			r.Body = io.NopCloser(bytes.NewReader(make([]byte, chunkSize+1)))
			r.ContentLength = -1
		}, http.StatusRequestEntityTooLarge, ""},
		{"content past Upload-Length", func(r *http.Request) {
			r.Body = io.NopCloser(bytes.NewReader(make([]byte, 8)))
			r.ContentLength = 8
			r.Header.Set("Upload-Length", "4")
		}, http.StatusBadRequest, inconsistentLength},
		{"completing short of Upload-Length", func(r *http.Request) {
			r.Header.Set("Upload-Complete", "?1")
			r.Body = io.NopCloser(bytes.NewReader(make([]byte, 4)))
			r.ContentLength = 4
			r.Header.Set("Upload-Length", "8")
		}, http.StatusBadRequest, inconsistentLength},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			r := creation("?0", nil)
			c.edit(r)
			res := f.serve(r)
			if res.StatusCode != c.status {
				t.Fatalf("status %d, want %d", res.StatusCode, c.status)
			}
			wantIncomplete(t, res)
			if c.typ != "" {
				if p := problem(t, res); p.Type != c.typ {
					t.Errorf("problem type %q, want %q", p.Type, c.typ)
				}
			}
		})
	}
}

func TestCreateComplete(t *testing.T) {
	f := newFixture(t)
	file := []byte("whole file")
	r := creation("?1", file)
	res := f.serve(r)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want the completion's 200", res.StatusCode)
	}
	if got := res.Header.Get("Upload-Complete"); got != "?1" {
		t.Errorf("Upload-Complete = %q, want ?1", got)
	}
	if loc := res.Header.Get("Location"); loc != "" {
		t.Errorf("the completing response carries the upload's Location %q", loc)
	}
	files := f.done.all()
	if len(files) != 1 {
		t.Fatalf("Complete ran %d times, want once", len(files))
	}
	got := files[0]
	if got.MediaType != "application/pdf" || got.Digest != fileSum || got.Name != "report.pdf" || got.Length != int64(len(file)) {
		t.Errorf("Complete got %+v", got)
	}
	wantContent(t, f.store, got.ID, file)
}

func wantContent(t *testing.T, store upload.Store, id string, want []byte) {
	t.Helper()
	rc, err := store.Open(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("content %q, want %q", got, want)
	}
}

func TestHead(t *testing.T) {
	f := newFixture(t)
	known := f.create(40)
	f.serve(appendRequest(t, known, 0, "?0", []byte("abc")))
	unknown := f.create(-1)

	res := f.serve(httptest.NewRequest(http.MethodHead, known, nil))
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d, want 204", res.StatusCode)
	}
	if res.Header.Get("Upload-Offset") != "3" || res.Header.Get("Upload-Length") != "40" {
		t.Errorf("Upload-Offset %q, Upload-Length %q; want 3 and 40", res.Header.Get("Upload-Offset"), res.Header.Get("Upload-Length"))
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	wantLimits(t, res)

	res = f.serve(httptest.NewRequest(http.MethodHead, unknown, nil))
	if _, ok := res.Header["Upload-Length"]; ok || res.Header.Get("Upload-Offset") != "0" {
		t.Errorf("unknown length: Upload-Length %q, Upload-Offset %q", res.Header.Get("Upload-Length"), res.Header.Get("Upload-Offset"))
	}
}

func TestExpired(t *testing.T) {
	f := newFixture(t)
	id := uuid.NewV4().String()
	if err := f.store.Create(t.Context(), id, "application/pdf", fileSum, "", -1, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	loc := prefix + "/" + id
	for _, r := range []*http.Request{
		httptest.NewRequest(http.MethodHead, loc, nil),
		appendRequest(t, loc, 0, "?0", []byte("abc")),
		httptest.NewRequest(http.MethodDelete, loc, nil),
	} {
		if res := f.serve(r); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", r.Method, res.StatusCode)
		}
	}
}

func TestAppend(t *testing.T) {
	f := newFixture(t)
	loc := f.create(-1)
	parts := [][]byte{[]byte("hello, "), []byte("resumable "), []byte("world")}
	var offset int64
	for i, p := range parts {
		complete := "?0"
		if i == len(parts)-1 {
			complete = "?1"
		}
		res := f.serve(appendRequest(t, loc, offset, complete, p))
		offset += int64(len(p))
		if complete == "?1" {
			if res.StatusCode != http.StatusOK || res.Header.Get("Upload-Complete") != "?1" {
				t.Fatalf("completing append: status %d, Upload-Complete %q", res.StatusCode, res.Header.Get("Upload-Complete"))
			}
			break
		}
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("append %d: status %d, want 204", i, res.StatusCode)
		}
		wantIncomplete(t, res)
		if got := res.Header.Get("Upload-Offset"); got != strconv.FormatInt(offset, 10) {
			t.Errorf("append %d: Upload-Offset %q, want %d", i, got, offset)
		}
	}
	files := f.done.all()
	if len(files) != 1 || files[0].ID != id(loc) || files[0].Length != offset {
		t.Fatalf("Complete got %+v, want one call for %s at length %d", files, id(loc), offset)
	}
	wantContent(t, f.store, id(loc), []byte("hello, resumable world"))
}

func TestAppendMismatchedOffset(t *testing.T) {
	f := newFixture(t)
	loc := f.create(-1)
	f.serve(appendRequest(t, loc, 0, "?0", []byte("abc")))
	res := f.serve(appendRequest(t, loc, 7, "?0", []byte("def")))
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409", res.StatusCode)
	}
	wantIncomplete(t, res)
	if got := res.Header.Get("Upload-Offset"); got != "3" {
		t.Errorf("Upload-Offset = %q, want 3", got)
	}
	p := problem(t, res)
	if p.Type != mismatchingOffset {
		t.Errorf("problem type %q, want %q", p.Type, mismatchingOffset)
	}
	if p.Extras["expected-offset"] != 3.0 || p.Extras["provided-offset"] != 7.0 {
		t.Errorf("problem members %v, want expected-offset 3 and provided-offset 7", p.Extras)
	}
}

type brokenReader struct{ r io.Reader }

func (b brokenReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func TestAppendRefusals(t *testing.T) {
	chunk := []byte("abc")
	cases := []struct {
		name   string
		edit   func(*http.Request)
		status int
	}{
		{"no Upload-Offset", func(r *http.Request) { r.Header.Del("Upload-Offset") }, http.StatusBadRequest},
		{"negative Upload-Offset", func(r *http.Request) { r.Header.Set("Upload-Offset", "-1") }, http.StatusBadRequest},
		{"invalid Upload-Offset", func(r *http.Request) { r.Header.Set("Upload-Offset", "zero") }, http.StatusBadRequest},
		{"no Upload-Complete", func(r *http.Request) { r.Header.Del("Upload-Complete") }, http.StatusBadRequest},
		{"invalid Upload-Complete", func(r *http.Request) { r.Header.Set("Upload-Complete", "1") }, http.StatusBadRequest},
		{"Content-Encoding", func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, http.StatusUnsupportedMediaType},
		{"wrong Content-Type", func(r *http.Request) { r.Header.Set("Content-Type", "application/octet-stream") }, http.StatusUnsupportedMediaType},
		{"declared too large", func(r *http.Request) {
			r.Body = io.NopCloser(bytes.NewReader(make([]byte, chunkSize+1)))
			r.ContentLength = chunkSize + 1
		}, http.StatusRequestEntityTooLarge},
		{"too large", func(r *http.Request) {
			r.Body = io.NopCloser(bytes.NewReader(make([]byte, chunkSize+1)))
			r.ContentLength = -1
		}, http.StatusRequestEntityTooLarge},
		{"mismatched digest", func(r *http.Request) { r.Header.Set("Content-Digest", digestField(t, []byte("abd"))) }, http.StatusBadRequest},
		{"no digest", func(r *http.Request) { r.Header.Del("Content-Digest") }, http.StatusBadRequest},
		{"interrupted", func(r *http.Request) {
			r.Body = io.NopCloser(brokenReader{bytes.NewReader(chunk)})
			r.ContentLength = -1
		}, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			loc := f.create(-1)
			r := appendRequest(t, loc, 0, "?0", chunk)
			c.edit(r)
			res := f.serve(r)
			if res.StatusCode != c.status {
				t.Fatalf("status %d, want %d", res.StatusCode, c.status)
			}
			wantIncomplete(t, res)
			if got := f.offset(loc); got != 0 {
				t.Errorf("offset = %d after a refusal, want 0", got)
			}
		})
	}
}

func TestAppendPastLength(t *testing.T) {
	for _, declared := range []bool{true, false} {
		t.Run("declared="+strconv.FormatBool(declared), func(t *testing.T) {
			f := newFixture(t)
			loc := f.create(4)
			r := appendRequest(t, loc, 0, "?0", []byte("abcdef"))
			if !declared {
				r.ContentLength = -1
			}
			res := f.serve(r)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", res.StatusCode)
			}
			wantIncomplete(t, res)
			if p := problem(t, res); p.Type != inconsistentLength {
				t.Errorf("problem type %q, want %q", p.Type, inconsistentLength)
			}
			if res := f.serve(httptest.NewRequest(http.MethodHead, loc, nil)); res.StatusCode != http.StatusNotFound {
				t.Errorf("HEAD after the refusal: status %d, want 404 for a deactivated upload", res.StatusCode)
			}
		})
	}
}

func TestAppendInconsistentLength(t *testing.T) {
	cases := []struct {
		name    string
		created int64
		edit    func(*http.Request)
	}{
		{"Upload-Length differs", 40, func(r *http.Request) { r.Header.Set("Upload-Length", "41") }},
		{"completing short of the length", 40, func(r *http.Request) { r.Header.Set("Upload-Complete", "?1") }},
		{"completion and Upload-Length disagree", -1, func(r *http.Request) {
			r.Header.Set("Upload-Complete", "?1")
			r.Header.Set("Upload-Length", "9")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			loc := f.create(c.created)
			r := appendRequest(t, loc, 0, "?0", []byte("abc"))
			c.edit(r)
			res := f.serve(r)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", res.StatusCode)
			}
			wantIncomplete(t, res)
			if p := problem(t, res); p.Type != inconsistentLength {
				t.Errorf("problem type %q, want %q", p.Type, inconsistentLength)
			}
			if got := f.offset(loc); got != 0 {
				t.Errorf("offset = %d, want 0", got)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	f := newFixture(t)
	loc := f.create(-1)
	if res := f.serve(httptest.NewRequest(http.MethodDelete, loc, nil)); res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d, want 204", res.StatusCode)
	}
	for _, r := range []*http.Request{
		httptest.NewRequest(http.MethodHead, loc, nil),
		appendRequest(t, loc, 0, "?0", []byte("abc")),
		httptest.NewRequest(http.MethodDelete, loc, nil),
	} {
		if res := f.serve(r); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s after DELETE: status %d, want 404", r.Method, res.StatusCode)
		}
	}
}

type gatedReader struct {
	r       io.Reader
	started chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (g *gatedReader) Read(p []byte) (int, error) {
	g.once.Do(func() {
		g.started <- struct{}{}
		<-g.release
	})
	return g.r.Read(p)
}

func TestReplicas(t *testing.T) {
	store := upload.NewMemoryStore()
	done := &completions{}
	replicas := []http.Handler{handlerOver(store, done), handlerOver(store, done)}
	f := &fixture{t: t, store: store, handler: replicas[0], done: done}
	loc := f.create(-1)

	chunks := [][]byte{[]byte("from replica a"), []byte("from replica b")}
	started := make(chan struct{}, len(replicas))
	release := make(chan struct{})
	statuses := make([]int, len(replicas))
	var wg sync.WaitGroup
	for i, h := range replicas {
		r := appendRequest(t, loc, 0, "?0", chunks[i])
		r.Body = io.NopCloser(&gatedReader{r: bytes.NewReader(chunks[i]), started: started, release: release})
		wg.Go(func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			statuses[i] = rec.Code
		})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for range replicas {
		select {
		case <-started:
		case <-ctx.Done():
			close(release)
			wg.Wait()
			t.Fatal("both appends did not pass the offset check before either read its content")
		}
	}
	close(release)
	wg.Wait()

	committed := -1
	for i, s := range statuses {
		switch s {
		case http.StatusNoContent:
			if committed >= 0 {
				t.Fatal("both appends committed at one offset")
			}
			committed = i
		case http.StatusConflict:
		default:
			t.Fatalf("replica %d answered %d, want 204 or 409", i, s)
		}
	}
	if committed < 0 {
		t.Fatal("neither append committed")
	}
	wantContent(t, store, id(loc), chunks[committed])
}

func TestRoutesRequiresFields(t *testing.T) {
	full := func() upload.Handler {
		return upload.Handler{
			Store:    upload.NewMemoryStore(),
			Transfer: web.NewTransfer(chunkSize, 1<<20, time.Second),
			Lifetime: time.Hour,
			Complete: (&completions{}).complete,
			Logger:   slog.New(slog.DiscardHandler),
		}
	}
	cases := []struct {
		want string
		edit func(*upload.Handler)
	}{
		{"upload: Handler requires a Store", func(h *upload.Handler) { h.Store = nil }},
		{"upload: Handler requires a Transfer", func(h *upload.Handler) { h.Transfer = web.Transfer{} }},
		{"upload: Handler requires a Transfer whose limit", func(h *upload.Handler) { h.Transfer = web.NewTransfer(0, 1, 0) }},
		{"upload: Handler requires a positive Lifetime", func(h *upload.Handler) { h.Lifetime = 0 }},
		{"upload: Handler requires a Complete function", func(h *upload.Handler) { h.Complete = nil }},
		{"upload: Handler requires a Logger", func(h *upload.Handler) { h.Logger = nil }},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			h := full()
			c.edit(&h)
			defer func() {
				msg, _ := recover().(string)
				if !strings.HasPrefix(msg, c.want) {
					t.Errorf("panic %q, want one starting %q", msg, c.want)
				}
			}()
			h.Routes("/uploads")
		})
	}
}
