package upload_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/upload"
)

func fileOf(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a' + byte(i%26)
	}
	return b
}

func bounded(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func handlerLimited(limit int64, done *completions) http.Handler {
	h := upload.Handler{
		Store:    upload.NewMemoryStore(),
		Transfer: web.NewTransfer(limit, 1<<20, time.Second),
		Lifetime: time.Hour,
		Complete: done.complete,
		Logger:   slog.New(slog.DiscardHandler),
	}
	api := web.NewGroup("/api")
	api.Mount(h.Routes("/uploads"))
	return web.NewModule(api)
}

type seen struct {
	Method  string
	Offset  string
	Length  int64
	Interop string
	Status  int
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if code >= 200 && s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

type journal struct {
	next   http.Handler
	before func(n int, r *http.Request)
	after  func(n int, r *http.Request)
	mu     sync.Mutex
	log    []seen
	counts map[string]int
}

func (j *journal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	j.mu.Lock()
	if j.counts == nil {
		j.counts = map[string]int{}
	}
	n := j.counts[r.Method]
	j.counts[r.Method]++
	j.mu.Unlock()
	if j.before != nil {
		j.before(n, r)
	}
	sw := &statusWriter{ResponseWriter: w}
	j.next.ServeHTTP(sw, r)
	if j.after != nil {
		j.after(n, r)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.log = append(j.log, seen{
		Method:  r.Method,
		Offset:  r.Header.Get("Upload-Offset"),
		Length:  r.ContentLength,
		Interop: r.Header.Get("Upload-Draft-Interop-Version"),
		Status:  sw.status,
	})
}

func (j *journal) entries() []seen {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.log)
}

func (j *journal) appends() []int64 {
	var sizes []int64
	for _, e := range j.entries() {
		if e.Method == http.MethodPatch {
			sizes = append(sizes, e.Length)
		}
	}
	return sizes
}

func client(srv *httptest.Server) upload.Client {
	return upload.Client{HTTP: srv.Client(), Attempts: 3}
}

func wantFile(t *testing.T, done *completions, file []byte) {
	t.Helper()
	files := done.all()
	if len(files) != 1 {
		t.Fatalf("Complete ran %d times, want once", len(files))
	}
	if !bytes.Equal(done.content(0), file) {
		t.Errorf("Complete read %q, want %q", done.content(0), file)
	}
	if files[0].Length != int64(len(file)) || files[0].Digest != sum(file) {
		t.Errorf("Complete got length %d and digest %s, want %d and %s", files[0].Length, files[0].Digest, len(file), sum(file))
	}
}

func wantStored(t *testing.T, res *http.Response, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "stored" {
		t.Errorf("final response %d %q, want Complete's 200 \"stored\"", res.StatusCode, body)
	}
	if got := res.Header.Get("Upload-Complete"); got != "?1" {
		t.Errorf("final Upload-Complete = %q, want ?1", got)
	}
}

func TestClientUpload(t *testing.T) {
	done := &completions{}
	j := &journal{next: handlerLimited(16, done)}
	srv := httptest.NewServer(j)
	defer srv.Close()
	file := fileOf(40)

	ctx := bounded(t)
	r, err := client(srv).Create(ctx, srv.URL+prefix, "application/pdf", "report.pdf", bytes.NewReader(file))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if r.Location() == "" || r.Location()[:len(srv.URL+prefix)] != srv.URL+prefix {
		t.Errorf("Location = %q, want an absolute URL under %s", r.Location(), srv.URL+prefix)
	}
	res, err := r.Send(ctx)
	wantStored(t, res, err)
	wantFile(t, done, file)
	if f := done.all()[0]; f.MediaType != "application/pdf" || f.Name != "report.pdf" {
		t.Errorf("Complete got type %q and name %q, want the creation's", f.MediaType, f.Name)
	}

	interop := strconv.Itoa(upload.InteropVersion)
	want := []seen{
		{Method: http.MethodPost, Length: 0, Interop: interop, Status: http.StatusCreated},
		{Method: http.MethodPatch, Offset: "0", Length: 16, Interop: interop, Status: http.StatusNoContent},
		{Method: http.MethodPatch, Offset: "16", Length: 16, Interop: interop, Status: http.StatusNoContent},
		{Method: http.MethodPatch, Offset: "32", Length: 8, Interop: interop, Status: http.StatusOK},
	}
	if got := j.entries(); !slices.Equal(got, want) {
		t.Errorf("requests %+v, want %+v", got, want)
	}
}

func TestClientEmptyFile(t *testing.T) {
	done := &completions{}
	srv := httptest.NewServer(handlerLimited(16, done))
	defer srv.Close()
	ctx := bounded(t)
	r, err := client(srv).Create(ctx, srv.URL+prefix, "text/plain", "", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	res, err := r.Send(ctx)
	wantStored(t, res, err)
	wantFile(t, done, []byte{})
}

func TestClientChunks(t *testing.T) {
	for _, tc := range []struct {
		name      string
		limit     int64
		chunkSize int64
		want      []int64
	}{
		{"max-append-size", 5, 0, []int64{5, 5, 5, 5, 3}},
		{"ChunkSize below max-append-size", 16, 4, []int64{4, 4, 4, 4, 4, 3}},
		{"max-append-size below ChunkSize", 5, 8, []int64{5, 5, 5, 5, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done := &completions{}
			j := &journal{next: handlerLimited(tc.limit, done)}
			srv := httptest.NewServer(j)
			defer srv.Close()
			file := fileOf(23)
			c := client(srv)
			c.ChunkSize = tc.chunkSize
			ctx := bounded(t)
			r, err := c.Create(ctx, srv.URL+prefix, "text/plain", "", bytes.NewReader(file))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			res, err := r.Send(ctx)
			wantStored(t, res, err)
			wantFile(t, done, file)
			if got := j.appends(); !slices.Equal(got, tc.want) {
				t.Errorf("appends of %v bytes, want %v", got, tc.want)
			}
		})
	}
}

type forwarder struct {
	addr   string
	target string
	keep   int
	severs atomic.Int64
	ln     net.Listener
	mu     sync.Mutex
	conns  []net.Conn
	wg     sync.WaitGroup
}

func newForwarder(t *testing.T, target string, keep int) *forwarder {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &forwarder{addr: "http://" + ln.Addr().String(), target: target, keep: keep, ln: ln}
	f.wg.Go(f.accept)
	t.Cleanup(func() {
		_ = ln.Close()
		f.mu.Lock()
		for _, c := range f.conns {
			_ = c.Close()
		}
		f.mu.Unlock()
		f.wg.Wait()
	})
	return f
}

func (f *forwarder) accept() {
	for {
		client, err := f.ln.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp", f.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		f.mu.Lock()
		f.conns = append(f.conns, client, server)
		f.mu.Unlock()
		f.wg.Go(func() {
			_, _ = io.Copy(client, server)
			_ = client.Close()
		})
		f.wg.Go(func() {
			f.forward(client, server)
			_ = server.Close()
			_ = client.Close()
		})
	}
}

func (f *forwarder) forward(client, server net.Conn) {
	var since []byte
	buf := make([]byte, 32<<10)
	for {
		n, err := client.Read(buf)
		if n > 0 {
			p := buf[:n]
			if f.severs.Load() <= 0 {
				since = nil
			} else {
				prev := len(since)
				since = append(since, p...)
				if cut, ok := f.cutPoint(since); ok {
					f.severs.Add(-1)
					_, _ = server.Write(since[prev:cut])
					return
				}
			}
			if _, err := server.Write(p); err != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (f *forwarder) cutPoint(stream []byte) (int, bool) {
	start := bytes.Index(stream, []byte("PATCH "))
	if start < 0 {
		return 0, false
	}
	end := bytes.Index(stream[start:], []byte("\r\n\r\n"))
	if end < 0 {
		return 0, false
	}
	cut := start + end + 4 + f.keep
	return cut, len(stream) > cut
}

func TestClientResumesAfterSeveredAppend(t *testing.T) {
	done := &completions{}
	var fwd *forwarder
	j := &journal{next: handlerLimited(16, done)}
	j.after = func(n int, r *http.Request) {
		if r.Method == http.MethodPatch && n == 1 {
			fwd.severs.Store(1)
		}
	}
	srv := httptest.NewServer(j)
	defer srv.Close()
	fwd = newForwarder(t, srv.Listener.Addr().String(), 5)
	file := fileOf(100)

	ctx := bounded(t)
	r, err := client(srv).Create(ctx, fwd.addr+prefix, "text/plain", "", bytes.NewReader(file))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	res, err := r.Send(ctx)
	wantStored(t, res, err)
	if fwd.severs.Load() != 0 {
		t.Fatal("the forwarder severed no append")
	}
	wantFile(t, done, file)

	var heads, at32 int
	for _, e := range j.entries() {
		switch {
		case e.Method == http.MethodHead:
			heads++
			if e.Status != http.StatusNoContent {
				t.Errorf("HEAD answered %d, want 204", e.Status)
			}
		case e.Method == http.MethodPatch && e.Offset == "32":
			at32++
		}
	}
	if heads != 1 {
		t.Errorf("%d offset retrievals, want 1", heads)
	}
	if at32 != 2 {
		t.Errorf("%d appends at offset 32, want the severed one and its resumption", at32)
	}
}

type finalDropper struct {
	http.ResponseWriter
	dropped bool
}

func (d *finalDropper) WriteHeader(code int) {
	if code < 200 {
		d.ResponseWriter.WriteHeader(code)
		return
	}
	if conn, _, err := d.ResponseWriter.(http.Hijacker).Hijack(); err == nil {
		_ = conn.Close()
	}
	d.dropped = true
}

func (d *finalDropper) Write(p []byte) (int, error) {
	if !d.dropped {
		d.WriteHeader(http.StatusOK)
	}
	return len(p), nil
}

func TestClientLocationFromInterim(t *testing.T) {
	done := &completions{}
	j := &journal{next: handlerLimited(16, done)}
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			dropped := false
			once.Do(func() {
				dropped = true
				j.ServeHTTP(&finalDropper{ResponseWriter: w}, r)
			})
			if dropped {
				return
			}
		}
		j.ServeHTTP(w, r)
	}))
	defer srv.Close()
	file := fileOf(20)

	ctx := bounded(t)
	r, err := client(srv).Create(ctx, srv.URL+prefix, "text/plain", "", bytes.NewReader(file))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	res, err := r.Send(ctx)
	wantStored(t, res, err)
	wantFile(t, done, file)

	var methods []string
	for _, e := range j.entries() {
		methods = append(methods, e.Method)
	}
	want := []string{http.MethodPost, http.MethodHead, http.MethodPatch, http.MethodPatch}
	if !slices.Equal(methods, want) {
		t.Errorf("requests %v, want one creation, the offset retrieval at the 104's Location, and the appends", methods)
	}
}

func TestClientRacingWriter(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chunk func(file []byte) []byte
		final int
	}{
		{"the same bytes", func(file []byte) []byte { return file[16:32] }, http.StatusOK},
		{"other bytes", func([]byte) []byte { return bytes.Repeat([]byte("x"), 16) }, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done := &completions{}
			file := fileOf(40)
			inner := handlerLimited(16, done)
			var racer int
			j := &journal{next: inner}
			j.before = func(n int, r *http.Request) {
				if r.Method != http.MethodPatch || n != 1 {
					return
				}
				race := appendRequest(t, r.URL.Path, 16, "?0", tc.chunk(file))
				rec := httptest.NewRecorder()
				inner.ServeHTTP(rec, race)
				racer = rec.Code
			}
			srv := httptest.NewServer(j)
			defer srv.Close()

			ctx := bounded(t)
			r, err := client(srv).Create(ctx, srv.URL+prefix, "text/plain", "", bytes.NewReader(file))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			res, err := r.Send(ctx)
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			defer func() { _ = res.Body.Close() }()
			if racer != http.StatusNoContent {
				t.Fatalf("the racing append answered %d, want 204", racer)
			}

			var statuses []int
			for _, e := range j.entries() {
				if e.Method == http.MethodPatch {
					statuses = append(statuses, e.Status)
				}
			}
			if want := []int{http.StatusNoContent, http.StatusConflict, tc.final}; !slices.Equal(statuses, want) {
				t.Errorf("append statuses %v, want %v: the 409 for the race, then the completing append from its offset", statuses, want)
			}
			if res.StatusCode != tc.final || res.Header.Get("Upload-Complete") != "?1" {
				t.Errorf("final response %d with Upload-Complete %q, want %d with ?1", res.StatusCode, res.Header.Get("Upload-Complete"), tc.final)
			}
			if tc.final == http.StatusOK {
				wantFile(t, done, file)
			} else if n := len(done.all()); n != 0 {
				t.Errorf("Complete ran %d times on a file that fails its Repr-Digest", n)
			}
		})
	}
}

func TestClientCancel(t *testing.T) {
	srv := httptest.NewServer(handlerLimited(16, &completions{}))
	defer srv.Close()
	ctx := bounded(t)
	r, err := client(srv).Create(ctx, srv.URL+prefix, "text/plain", "", bytes.NewReader(fileOf(40)))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.Cancel(ctx); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	head, err := http.NewRequestWithContext(ctx, http.MethodHead, r.Location(), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Do(head)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("HEAD after the cancellation answered %d, want 404", res.StatusCode)
	}
	if _, err := r.Send(ctx); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Send after the cancellation: %v, want fs.ErrNotExist", err)
	}
	if err := r.Cancel(ctx); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a second Cancel: %v, want fs.ErrNotExist", err)
	}
}

func TestClientGivesUp(t *testing.T) {
	srv := httptest.NewServer(handlerLimited(16, &completions{}))
	defer srv.Close()
	fwd := newForwarder(t, srv.Listener.Addr().String(), 5)
	const severs = 100
	fwd.severs.Store(severs)

	ctx := bounded(t)
	c := client(srv)
	r, err := c.Create(ctx, fwd.addr+prefix, "text/plain", "", bytes.NewReader(fileOf(40)))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = r.Send(ctx)
	if err == nil || ctx.Err() != nil {
		t.Fatalf("Send: %v with the context %v, want an error within the attempts", err, ctx.Err())
	}
	if sent := severs - fwd.severs.Load(); sent != int64(c.Attempts) {
		t.Errorf("%d appends sent, want the %d attempts", sent, c.Attempts)
	}

	fwd.severs.Store(0)
	head, err := http.NewRequestWithContext(ctx, http.MethodHead, r.Location(), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Do(head)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Upload-Offset") != "0" {
		t.Errorf("HEAD after giving up answered %d at offset %q, want the upload left at 0", res.StatusCode, res.Header.Get("Upload-Offset"))
	}
	if err := r.Cancel(ctx); err != nil {
		t.Errorf("Cancel after giving up: %v", err)
	}
}

func TestClientRequiresFields(t *testing.T) {
	for _, tc := range []struct {
		c    upload.Client
		want string
	}{
		{upload.Client{Attempts: 1}, "upload: Client.Create requires an HTTP client"},
		{upload.Client{HTTP: http.DefaultClient}, "upload: Client.Create requires a positive Attempts"},
	} {
		func() {
			defer func() {
				if got := recover(); got != tc.want {
					t.Errorf("panic %v, want %q", got, tc.want)
				}
			}()
			_, _ = tc.c.Create(t.Context(), "http://127.0.0.1:0"+prefix, "text/plain", "", bytes.NewReader(nil))
		}()
	}
}

func TestClientCreationRefused(t *testing.T) {
	j := &journal{next: handlerLimited(16, &completions{})}
	srv := httptest.NewServer(j)
	defer srv.Close()
	_, err := client(srv).Create(bounded(t), srv.URL+prefix, "", "", bytes.NewReader(fileOf(3)))
	if err == nil {
		t.Fatal("Create without a media type succeeded, want the handler's 415")
	}
	want := []seen{{Method: http.MethodPost, Interop: strconv.Itoa(upload.InteropVersion), Status: http.StatusUnsupportedMediaType}}
	if got := j.entries(); !slices.Equal(got, want) {
		t.Errorf("requests %+v, want the one creation the 415 answers, not repeated", got)
	}
}

func TestClientRepeatsCreation(t *testing.T) {
	done := &completions{}
	j := &journal{next: handlerLimited(16, done)}
	var dropped atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && dropped.CompareAndSwap(false, true) {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		j.ServeHTTP(w, r)
	}))
	defer srv.Close()
	file := fileOf(20)

	ctx := bounded(t)
	r, err := client(srv).Create(ctx, srv.URL+prefix, "text/plain", "", bytes.NewReader(file))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !dropped.Load() {
		t.Fatal("no creation was dropped")
	}
	res, err := r.Send(ctx)
	wantStored(t, res, err)
	wantFile(t, done, file)
}

type lineSplitter struct {
	http.ResponseWriter
	name  string
	when  func(code int) bool
	split func(value string) []string
}

func (s *lineSplitter) WriteHeader(code int) {
	h := s.Header()
	if v := h.Get(s.name); v != "" && s.when(code) {
		h[http.CanonicalHeaderKey(s.name)] = s.split(v)
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *lineSplitter) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

func splitting(next http.Handler, name string, when func(code int) bool, split func(value string) []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&lineSplitter{ResponseWriter: w, name: name, when: when, split: split}, r)
	})
}

func always(int) bool { return true }

func TestClientUploadLimitLines(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int64
		lines func(value string) []string
		want  []int64
	}{
		{
			"split across two lines",
			1024,
			func(v string) []string {
				appendSize, age, _ := strings.Cut(v, ", ")
				return []string{age, appendSize}
			},
			[]int64{1024, 1024, 952},
		},
		{
			"a known member of the wrong type",
			4096,
			func(v string) []string { return []string{"max-append-size=1024", "max-age=?0"} },
			[]int64{3000},
		},
		{
			"a known member out of range",
			4096,
			func(v string) []string { return []string{"max-append-size=1024, max-age=-1"} },
			[]int64{3000},
		},
		{
			"an invalid combined range",
			4096,
			func(v string) []string { return []string{"max-append-size=1024, min-append-size=2048"} },
			[]int64{3000},
		},
		{
			"an unknown member",
			4096,
			func(v string) []string { return []string{"max-append-size=1024, flavor=?0"} },
			[]int64{1024, 1024, 952},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done := &completions{}
			j := &journal{next: splitting(handlerLimited(tc.limit, done), "Upload-Limit", always, tc.lines)}
			srv := httptest.NewServer(j)
			defer srv.Close()
			file := fileOf(3000)

			ctx := bounded(t)
			r, err := client(srv).Create(ctx, srv.URL+prefix, "text/plain", "", bytes.NewReader(file))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			res, err := r.Send(ctx)
			wantStored(t, res, err)
			wantFile(t, done, file)
			if got := j.appends(); !slices.Equal(got, tc.want) {
				t.Errorf("appends of %v bytes, want %v", got, tc.want)
			}
		})
	}
}

func TestClientSplitOffsetOnConflict(t *testing.T) {
	done := &completions{}
	file := fileOf(40)
	inner := handlerLimited(16, done)
	conflict := func(code int) bool { return code == http.StatusConflict }
	j := &journal{next: splitting(inner, "Upload-Offset", conflict, func(v string) []string {
		return []string{v[:1], v[1:]}
	})}
	j.before = func(n int, r *http.Request) {
		if r.Method != http.MethodPatch || n != 1 {
			return
		}
		rec := httptest.NewRecorder()
		inner.ServeHTTP(rec, appendRequest(t, r.URL.Path, 16, "?0", file[16:32]))
	}
	srv := httptest.NewServer(j)
	defer srv.Close()

	ctx := bounded(t)
	r, err := client(srv).Create(ctx, srv.URL+prefix, "text/plain", "", bytes.NewReader(file))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	res, err := r.Send(ctx)
	wantStored(t, res, err)
	wantFile(t, done, file)

	var got []string
	for _, e := range j.entries() {
		got = append(got, e.Method+" "+strconv.Itoa(e.Status))
	}
	want := []string{"POST 201", "PATCH 204", "PATCH 409", "HEAD 204", "PATCH 200"}
	if !slices.Equal(got, want) {
		t.Errorf("requests %v, want %v: the 409's split Upload-Offset read as missing, so an offset retrieval", got, want)
	}
}
