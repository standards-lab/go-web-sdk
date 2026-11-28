package web_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/go-core/sse"

	"github.com/standards-lab/go-web-sdk"
)

func eventStream(t *testing.T, cfg web.Config) web.EventStream {
	t.Helper()
	cfg = finalized(t, cfg)
	return cfg.EventStream(web.EventStreamOptions{})
}

func openStream(t *testing.T, ctx context.Context, url string) *sse.Decoder {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	return sse.NewDecoder(resp.Body)
}

func nextEvent(t *testing.T, d *sse.Decoder) sse.Event {
	t.Helper()
	e, err := d.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return e
}

func numbered(i int) sse.Event {
	n := strconv.Itoa(i)
	return sse.Event{Type: "tick", Data: []byte("data " + n + "\nline two"), ID: n}
}

func sameEvent(got, want sse.Event) bool {
	return got.Type == want.Type && got.ID == want.ID && bytes.Equal(got.Data, want.Data)
}

func decodeAll(t *testing.T, body []byte) []sse.Event {
	t.Helper()
	d := sse.NewDecoder(bytes.NewReader(body))
	var got []sse.Event
	for {
		e, err := d.Next()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		got = append(got, e)
	}
}

func waitOrTimeout(ch <-chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-time.After(failsafe):
		return errors.New("timed out")
	}
}

func TestEventStream_FlushesEachEventAsItIsSent(t *testing.T) {
	const n = 3
	es := eventStream(t, web.Config{})
	ack := make(chan struct{})
	served := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served <- es.Serve(w, r, func(_ context.Context, s *web.EventSender) error {
			if err := s.Retry(1500 * time.Millisecond); err != nil {
				return err
			}
			for i := range n {
				if err := s.Send(numbered(i)); err != nil {
					return err
				}
				if err := waitOrTimeout(ack); err != nil {
					return err
				}
			}
			return nil
		})
	}))
	defer srv.Close()

	d := openStream(t, t.Context(), srv.URL)
	for i := range n {
		if got := nextEvent(t, d); !sameEvent(got, numbered(i)) {
			t.Errorf("event %d = %+v, want %+v", i, got, numbered(i))
		}
		if retry, ok := d.Retry(); !ok || retry != 1500*time.Millisecond {
			t.Errorf("Retry = %s, %t, want 1.5s, true", retry, ok)
		}
		ack <- struct{}{}
	}
	if e, err := d.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("after the last event: Next = %+v, %v, want io.EOF", e, err)
	}
	if err := recvOrFail(t, served, "Serve to return"); err != nil {
		t.Errorf("Serve = %v, want nil", err)
	}
}

func TestEventStream_OutlastsWriteTimeout(t *testing.T) {
	const n, gap = 4, 100 * time.Millisecond
	cfg := web.Config{Host: "127.0.0.1", Port: new(0), WriteTimeout: dur(gap)}
	es := eventStream(t, cfg)
	served := make(chan error, 1)
	srv := startServer(t, cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served <- es.Serve(w, r, func(_ context.Context, s *web.EventSender) error {
			for i := range n {
				if i > 0 {
					time.Sleep(gap)
				}
				if err := s.Send(numbered(i)); err != nil {
					return err
				}
			}
			return nil
		})
	}))

	d := openStream(t, t.Context(), "http://"+srv.Addr()+"/")
	for i := range n {
		if got := nextEvent(t, d); !sameEvent(got, numbered(i)) {
			t.Errorf("event %d = %+v, want %+v", i, got, numbered(i))
		}
	}
	if err := recvOrFail(t, served, "Serve to return"); err != nil {
		t.Errorf("Serve = %v, want nil: a %s stream must outlast a %s WriteTimeout", err, (n-1)*gap, gap)
	}
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	mu        sync.Mutex
	deadlines []time.Time
	err       error
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deadlines = append(d.deadlines, t)
	return d.err
}

func TestEventStream_SetsWriteDeadlineBeforeEachWrite(t *testing.T) {
	es := eventStream(t, web.Config{WriteTimeout: dur(time.Hour)})
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now()
	err := es.Serve(w, httptest.NewRequest(http.MethodGet, "/", nil), func(_ context.Context, s *web.EventSender) error {
		if err := s.Send(numbered(0)); err != nil {
			return err
		}
		return s.Retry(time.Second)
	})
	if err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if len(w.deadlines) != 3 {
		t.Fatalf("deadlines set = %d, want 3: the headers, the event, and the retry", len(w.deadlines))
	}
	for i, dl := range w.deadlines {
		if dl.Before(before.Add(time.Hour)) || dl.After(time.Now().Add(time.Hour)) {
			t.Errorf("deadline %d = %s, want the write timeout from the write", i, dl)
		}
	}

	disabled := eventStream(t, web.Config{WriteTimeout: dur(0)})
	w = &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	err = disabled.Serve(w, httptest.NewRequest(http.MethodGet, "/", nil), func(_ context.Context, s *web.EventSender) error {
		return s.Send(numbered(0))
	})
	if err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if len(w.deadlines) != 0 {
		t.Errorf("deadlines set = %d under a disabled WriteTimeout, want none", len(w.deadlines))
	}
}

func TestEventStream_ReturnsADeadlineError(t *testing.T) {
	es := eventStream(t, web.Config{})
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder(), err: errors.New("connection gone")}
	called := false
	err := es.Serve(w, httptest.NewRequest(http.MethodGet, "/", nil), func(context.Context, *web.EventSender) error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "connection gone") {
		t.Errorf("Serve = %v, want the deadline error", err)
	}
	if called {
		t.Error("Serve called fn after the headers' deadline failed")
	}
}

func TestEventStream_ServesOverARecorderWithoutDeadlines(t *testing.T) {
	es := eventStream(t, web.Config{})
	rec := httptest.NewRecorder()
	err := es.Serve(rec, httptest.NewRequest(http.MethodGet, "/", nil), func(_ context.Context, s *web.EventSender) error {
		for i := range 2 {
			if err := s.Send(numbered(i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Serve = %v, want nil: a writer without deadlines is tolerated", err)
	}
	if !rec.Flushed {
		t.Error("the recorder was never flushed")
	}
	got := decodeAll(t, rec.Body.Bytes())
	if len(got) != 2 || !sameEvent(got[0], numbered(0)) || !sameEvent(got[1], numbered(1)) {
		t.Errorf("events = %+v, want events 0 and 1", got)
	}
}

type unflushable struct{ http.ResponseWriter }

func TestEventStream_RefusesAWriterThatCannotFlush(t *testing.T) {
	es := eventStream(t, web.Config{})
	called := false
	err := es.Serve(unflushable{httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/", nil), func(context.Context, *web.EventSender) error {
		called = true
		return nil
	})
	if !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Serve = %v, want http.ErrNotSupported", err)
	}
	if called {
		t.Error("Serve called fn on a writer that cannot flush")
	}
}

func TestEventStream_ReturnsFnErrorAndWritesNothingAtTheEnd(t *testing.T) {
	es := eventStream(t, web.Config{})
	rec := httptest.NewRecorder()
	want := errors.New("producer failed")
	var body string
	err := es.Serve(rec, httptest.NewRequest(http.MethodGet, "/", nil), func(_ context.Context, s *web.EventSender) error {
		if err := s.Send(numbered(0)); err != nil {
			return err
		}
		body = rec.Body.String()
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("Serve = %v, want fn's error", err)
	}
	if rec.Body.String() != body {
		t.Errorf("body = %q after fn returned, want %q: nothing written at the end", rec.Body.String(), body)
	}
}

func TestEventSender_RejectionWritesNothingAndKeepsTheStream(t *testing.T) {
	es := eventStream(t, web.Config{})
	rec := httptest.NewRecorder()
	err := es.Serve(rec, httptest.NewRequest(http.MethodGet, "/", nil), func(_ context.Context, s *web.EventSender) error {
		if err := s.Send(sse.Event{ID: "a\nb", Data: []byte("x")}); err == nil {
			t.Error("Send of an id with a newline = nil, want the encoder's error")
		}
		if err := s.Retry(-time.Second); err == nil {
			t.Error("Retry of a negative duration = nil, want the encoder's error")
		}
		if rec.Body.Len() != 0 {
			t.Errorf("body = %q after the rejections, want nothing written", rec.Body.String())
		}
		return s.Send(numbered(0))
	})
	if err != nil {
		t.Fatalf("Serve = %v, want the send after the rejections to succeed", err)
	}
	if got := decodeAll(t, rec.Body.Bytes()); len(got) != 1 || !sameEvent(got[0], numbered(0)) {
		t.Errorf("events = %+v, want event 0 alone", got)
	}
}

type failingWriter struct {
	*httptest.ResponseRecorder
	fail bool
}

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.fail {
		return 0, errors.New("broken pipe")
	}
	return f.ResponseRecorder.Write(p)
}

func TestEventSender_WriteErrorClosesTheSender(t *testing.T) {
	es := eventStream(t, web.Config{})
	w := &failingWriter{ResponseRecorder: httptest.NewRecorder()}
	err := es.Serve(w, httptest.NewRequest(http.MethodGet, "/", nil), func(_ context.Context, s *web.EventSender) error {
		w.fail = true
		if err := s.Send(numbered(0)); err == nil || !strings.Contains(err.Error(), "broken pipe") {
			t.Errorf("Send on a failing writer = %v, want the write error", err)
		}
		w.fail = false
		if err := s.Send(numbered(1)); err == nil {
			t.Error("Send after a write error = nil, want the sender closed")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want nothing written after the failed write", w.Body.String())
	}
}

func TestEventSender_SendAfterServeReturnsFails(t *testing.T) {
	es := eventStream(t, web.Config{})
	rec := httptest.NewRecorder()
	var leaked *web.EventSender
	if err := es.Serve(rec, httptest.NewRequest(http.MethodGet, "/", nil), func(_ context.Context, s *web.EventSender) error {
		leaked = s
		return nil
	}); err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if err := leaked.Send(numbered(0)); err == nil {
		t.Error("Send after Serve returned = nil, want an error")
	}
	if err := leaked.Retry(time.Second); err == nil {
		t.Error("Retry after Serve returned = nil, want an error")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want nothing written", rec.Body.String())
	}
}

func TestEventSender_ConcurrentSends(t *testing.T) {
	const senders, each = 8, 10
	es := eventStream(t, web.Config{})
	rec := httptest.NewRecorder()
	err := es.Serve(rec, httptest.NewRequest(http.MethodGet, "/", nil), func(_ context.Context, s *web.EventSender) error {
		var wg sync.WaitGroup
		errs := make(chan error, senders*each)
		for g := range senders {
			wg.Go(func() {
				for i := range each {
					errs <- s.Send(numbered(g*each + i))
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if got := decodeAll(t, rec.Body.Bytes()); len(got) != senders*each {
		t.Errorf("decoded %d events, want %d whole events", len(got), senders*each)
	}
}

type ended struct {
	sendErr  error
	retryErr error
	reqErr   error
}

func endedStream(es web.EventStream, report chan<- ended, sent chan<- struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = es.Serve(w, r, func(ctx context.Context, s *web.EventSender) error {
			if err := s.Send(numbered(0)); err != nil {
				return err
			}
			close(sent)
			if err := waitOrTimeout(ctx.Done()); err != nil {
				return err
			}
			report <- ended{sendErr: s.Send(numbered(1)), retryErr: s.Retry(time.Second), reqErr: r.Context().Err()}
			return nil
		})
	})
}

func TestEventSender_SendAfterRequestEndsFails(t *testing.T) {
	report := make(chan ended, 1)
	sent := make(chan struct{})
	srv := httptest.NewServer(endedStream(eventStream(t, web.Config{}), report, sent))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	d := openStream(t, ctx, srv.URL)
	nextEvent(t, d)
	recvOrFail(t, sent, "the first send")
	cancel()

	got := recvOrFail(t, report, "the stream's context to end")
	if !errors.Is(got.sendErr, context.Canceled) {
		t.Errorf("Send after the request ended = %v, want context.Canceled", got.sendErr)
	}
	if got.retryErr == nil {
		t.Error("Retry after the request ended = nil, want an error")
	}
}

func TestEventSender_SendAfterRunContextEndsFails(t *testing.T) {
	run, stop := context.WithCancel(context.Background())
	defer stop()
	report := make(chan ended, 1)
	sent := make(chan struct{})
	cfg := web.Config{Host: "127.0.0.1", Port: new(0)}
	srv := startServerWithContext(t, run, cfg, endedStream(eventStream(t, cfg), report, sent))

	d := openStream(t, t.Context(), "http://"+srv.Addr()+"/")
	if got := nextEvent(t, d); !sameEvent(got, numbered(0)) {
		t.Errorf("event = %+v, want event 0: the stream is live while the run context is", got)
	}
	recvOrFail(t, sent, "the first send")
	stop()

	got := recvOrFail(t, report, "the stream's context to end")
	if !errors.Is(got.sendErr, context.Canceled) {
		t.Errorf("Send after the run context ended = %v, want context.Canceled", got.sendErr)
	}
	if got.retryErr == nil {
		t.Error("Retry after the run context ended = nil, want an error")
	}
	if got.reqErr != nil {
		t.Errorf("request context err = %v, want nil: the run context ends the stream, not the request", got.reqErr)
	}
	if e, err := d.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("after the run context ended: Next = %+v, %v, want io.EOF with nothing written", e, err)
	}
}

func TestService_DrainsAStream(t *testing.T) {
	var stderr bytes.Buffer
	s := newService(t)
	s.handler = endedStream(eventStream(t, web.Config{}), make(chan ended, 1), make(chan struct{}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := s.run(ctx, &stderr)

	if ready := s.log.next(t); !strings.Contains(ready, `msg="server ready"`) {
		t.Fatalf("first record = %q, want server ready", ready)
	}
	d := openStream(t, t.Context(), "http://"+s.builtServer().Addr()+"/")
	nextEvent(t, d)

	cancel()
	if code := recvOrFail(t, done, "Run to return within the shutdown budget"); code != 0 {
		t.Errorf("Run = %d, want 0: the stream must end when the run context does; log = %q", code, s.log.String())
	}
	if e, err := d.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("after the drain: Next = %+v, %v, want io.EOF", e, err)
	}
}

func keepAliveStream(t *testing.T, keepAlive time.Duration) web.EventStream {
	t.Helper()
	cfg := finalized(t, web.Config{})
	return cfg.EventStream(web.EventStreamOptions{KeepAlive: keepAlive})
}

func openRawStream(t *testing.T, url string) *bufio.Reader {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	return bufio.NewReader(resp.Body)
}

func commentLines(body []byte) int {
	n := 0
	for line := range strings.Lines(string(body)) {
		if strings.HasPrefix(line, ":") {
			n++
		}
	}
	return n
}

func TestEventStream_KeepsAQuietStreamAlive(t *testing.T) {
	const keepAlive, want = 20 * time.Millisecond, 3
	es := keepAliveStream(t, keepAlive)
	release := make(chan struct{})
	served := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served <- es.Serve(w, r, func(context.Context, *web.EventSender) error {
			return waitOrTimeout(release)
		})
	}))
	defer srv.Close()

	start := time.Now()
	body := openRawStream(t, srv.URL)
	var raw bytes.Buffer
	for comments := 0; comments < want; {
		line, err := body.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the stream after %d comments: %v", comments, err)
		}
		raw.WriteString(line)
		if strings.HasPrefix(line, ":") {
			comments++
		}
	}
	if elapsed := time.Since(start); elapsed < want*keepAlive {
		t.Errorf("%d keepalives arrived within %s, want one per %s", want, elapsed, keepAlive)
	}
	close(release)
	rest, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading the stream's end: %v", err)
	}
	raw.Write(rest)
	if err := recvOrFail(t, served, "Serve to return"); err != nil {
		t.Errorf("Serve = %v, want nil", err)
	}
	if got := decodeAll(t, raw.Bytes()); len(got) != 0 {
		t.Errorf("decoded %+v from a quiet stream, want no event", got)
	}
}

func TestEventStream_BusyStreamCarriesNoKeepAlive(t *testing.T) {
	const keepAlive = 100 * time.Millisecond
	es := keepAliveStream(t, keepAlive)
	served := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served <- es.Serve(w, r, func(_ context.Context, s *web.EventSender) error {
			deadline := time.Now().Add(3 * keepAlive)
			for i := 0; time.Now().Before(deadline); i++ {
				var err error
				if i%2 == 0 {
					err = s.Send(numbered(i))
				} else {
					err = s.Retry(time.Second)
				}
				if err != nil {
					return err
				}
				time.Sleep(keepAlive / 20)
			}
			return nil
		})
	}))
	defer srv.Close()

	raw, err := io.ReadAll(openRawStream(t, srv.URL))
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if err := recvOrFail(t, served, "Serve to return"); err != nil {
		t.Errorf("Serve = %v, want nil", err)
	}
	if n := commentLines(raw); n != 0 {
		t.Errorf("a stream sending every %s carried %d keepalives, want none", keepAlive/20, n)
	}
	if got := decodeAll(t, raw); len(got) == 0 {
		t.Error("decoded no event from a busy stream")
	}
}

func TestEventSender_ConcurrentSendsAgainstKeepAlives(t *testing.T) {
	const senders, each = 8, 20
	es := keepAliveStream(t, time.Millisecond)
	served := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served <- es.Serve(w, r, func(_ context.Context, s *web.EventSender) error {
			var wg sync.WaitGroup
			errs := make(chan error, senders*each)
			for g := range senders {
				wg.Go(func() {
					for i := range each {
						errs <- s.Send(numbered(g*each + i))
						time.Sleep(time.Millisecond)
					}
				})
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					return err
				}
			}
			return nil
		})
	}))
	defer srv.Close()

	raw, err := io.ReadAll(openRawStream(t, srv.URL))
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if err := recvOrFail(t, served, "Serve to return"); err != nil {
		t.Fatalf("Serve = %v", err)
	}
	got := decodeAll(t, raw)
	if len(got) != senders*each {
		t.Errorf("decoded %d events, want %d", len(got), senders*each)
	}
	for _, e := range got {
		id, err := strconv.Atoi(e.ID)
		if err != nil || !sameEvent(e, numbered(id)) {
			t.Errorf("event %+v is not whole", e)
		}
	}
}

type goneWriter struct {
	*httptest.ResponseRecorder
	gone   atomic.Bool
	failed chan struct{}
	once   sync.Once
}

func (g *goneWriter) Write(p []byte) (int, error) {
	if g.gone.Load() {
		g.once.Do(func() { close(g.failed) })
		return 0, errors.New("broken pipe")
	}
	return g.ResponseRecorder.Write(p)
}

func TestEventSender_KeepAliveFailureClosesTheSender(t *testing.T) {
	es := keepAliveStream(t, time.Millisecond)
	w := &goneWriter{ResponseRecorder: httptest.NewRecorder(), failed: make(chan struct{})}
	err := es.Serve(w, httptest.NewRequest(http.MethodGet, "/", nil), func(_ context.Context, s *web.EventSender) error {
		w.gone.Store(true)
		if err := waitOrTimeout(w.failed); err != nil {
			return err
		}
		w.gone.Store(false)
		if err := s.Send(numbered(0)); err == nil || !strings.Contains(err.Error(), "broken pipe") {
			t.Errorf("Send after a failed keepalive = %v, want the keepalive's write error", err)
		}
		if err := s.Retry(time.Second); err == nil {
			t.Error("Retry after a failed keepalive = nil, want the sender closed")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if got := decodeAll(t, w.Body.Bytes()); len(got) != 0 {
		t.Errorf("events = %+v, want nothing written after the failed keepalive", got)
	}
}

type watchedRecorder struct {
	*httptest.ResponseRecorder
	mu    sync.Mutex
	body  bytes.Buffer
	wrote chan struct{}
}

func newWatchedRecorder() *watchedRecorder {
	return &watchedRecorder{ResponseRecorder: httptest.NewRecorder(), wrote: make(chan struct{}, 1)}
}

func (w *watchedRecorder) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case w.wrote <- struct{}{}:
	default:
	}
	return w.body.Write(p)
}

func (w *watchedRecorder) snapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func TestEventStream_KeepAlivesStopWhenServeReturns(t *testing.T) {
	const keepAlive = time.Millisecond
	es := keepAliveStream(t, keepAlive)
	w := newWatchedRecorder()
	err := es.Serve(w, httptest.NewRequest(http.MethodGet, "/", nil), func(context.Context, *web.EventSender) error {
		return waitOrTimeout(w.wrote)
	})
	if err != nil {
		t.Fatalf("Serve = %v", err)
	}
	body := w.snapshot()
	if commentLines([]byte(body)) == 0 {
		t.Fatalf("body = %q, want a keepalive while fn ran", body)
	}
	time.Sleep(20 * keepAlive)
	if got := w.snapshot(); got != body {
		t.Errorf("body grew from %q to %q after Serve returned", body, got)
	}
}

func TestEventStream_KeepAlivesStopWhenTheStreamEnds(t *testing.T) {
	const keepAlive = time.Millisecond
	es := keepAliveStream(t, keepAlive)
	w := newWatchedRecorder()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	err := es.Serve(w, r, func(ctx context.Context, _ *web.EventSender) error {
		if err := waitOrTimeout(w.wrote); err != nil {
			return err
		}
		cancel()
		<-ctx.Done()
		time.Sleep(10 * keepAlive)
		body := w.snapshot()
		time.Sleep(20 * keepAlive)
		if got := w.snapshot(); got != body {
			t.Errorf("body grew from %q to %q after the stream's context ended", body, got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Serve = %v", err)
	}
}

func TestEventStream_PanicsOnADefect(t *testing.T) {
	panicsWith(t, "Config not finalized", func() {
		var cfg web.Config
		cfg.EventStream(web.EventStreamOptions{})
	})
	panicsWith(t, "KeepAlive", func() {
		cfg := finalized(t, web.Config{})
		cfg.EventStream(web.EventStreamOptions{KeepAlive: -time.Second})
	})
	panicsWith(t, "zero EventStream", func() {
		_ = web.EventStream{}.Serve(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), func(context.Context, *web.EventSender) error {
			return nil
		})
	})
}
