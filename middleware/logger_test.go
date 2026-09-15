package middleware_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// record serves one GET through the request logger and returns the single log
// record it emitted. The logger is a plain slog value: the middleware takes the
// standard library's type, not a logging package one.
func record(t *testing.T, handler http.Handler) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	webtest.Probe(web.Chain(handler, middleware.RequestLogger(logger)), "/orders/7")

	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	return out
}

func TestRequestLogger_DescribesTheRequest(t *testing.T) {
	out := record(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = web.WriteJSON(w, http.StatusCreated, map[string]string{"id": "7"})
	}))

	for _, tc := range []struct {
		key  string
		want any
	}{
		{"msg", "request"},
		{"level", "INFO"},
		{"http.request.method", http.MethodGet},
		{"url.path", "/orders/7"},
		{"http.response.status_code", float64(http.StatusCreated)},
		{"client.address", "192.0.2.1:1234"},
	} {
		if got := out[tc.key]; got != tc.want {
			t.Errorf("%s = %v, want %v", tc.key, got, tc.want)
		}
	}

	if _, ok := out["duration"].(float64); !ok {
		t.Errorf("duration = %v, want a number", out["duration"])
	}
}

// A handler that writes a body without calling WriteHeader sends 200
// implicitly, and the recorder is seeded with that same status.
func TestRequestLogger_ImplicitStatusIsOK(t *testing.T) {
	out := record(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	if got := out["http.response.status_code"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", got)
	}
}

// A handler that writes a body and only then calls WriteHeader has already
// committed the response — net/http sent the implicit 200 on that first
// Write — so the later WriteHeader call is superfluous and has no effect on
// the wire. The recorded status must be the one the client actually got.
func TestRequestLogger_WriteThenWriteHeaderRecordsTheCommittedStatus(t *testing.T) {
	out := record(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		w.WriteHeader(http.StatusInternalServerError)
	}))

	if got := out["http.response.status_code"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want the committed 200, not the superfluous 500", got)
	}
}

func TestRequestLogger_RecordsProblemStatus(t *testing.T) {
	out := record(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = web.WriteProblem(w, r, http.StatusTeapot, "", "no coffee")
	}))

	if got := out["http.response.status_code"]; got != float64(http.StatusTeapot) {
		t.Errorf("status = %v, want 418", got)
	}
}

// The status a client sees is the first one written; a superfluous second call
// must not rewrite what was logged.
func TestRequestLogger_FirstStatusWins(t *testing.T) {
	out := record(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	if got := out["http.response.status_code"]; got != float64(http.StatusAccepted) {
		t.Errorf("status = %v, want 202", got)
	}
}

// Wrapping the ResponseWriter must not cost a handler the capabilities the
// underlying writer has; http.ResponseController finds them through Unwrap.
func TestRequestLogger_ResponseControllerReachesTheWriter(t *testing.T) {
	var flushErr error
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chunk"))
		flushErr = http.NewResponseController(w).Flush()
	})

	rec := webtest.Probe(web.Chain(handler, middleware.RequestLogger(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))), "/")

	if flushErr != nil {
		t.Errorf("Flush through the wrapper: %v", flushErr)
	}
	if !rec.Flushed {
		t.Error("the underlying recorder was not flushed")
	}
}

func TestRequestLogger_WriterSupportsReadFrom(t *testing.T) {
	var isReaderFrom bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, isReaderFrom = w.(io.ReaderFrom)
		_, _ = io.Copy(w, strings.NewReader("streamed body"))
	})

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	rec := webtest.Probe(web.Chain(handler, middleware.RequestLogger(logger)), "/")

	if !isReaderFrom {
		t.Error("the wrapped writer does not implement io.ReaderFrom")
	}
	if got := rec.Body.String(); got != "streamed body" {
		t.Errorf("body = %q, want %q", got, "streamed body")
	}

	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	if got := out["http.response.status_code"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", got)
	}
}

// A successful probe request is orchestrator heartbeat, not traffic: it logs
// at debug, so a production logger at info stays quiet.
func TestRequestLogger_ProbeSuccessLogsAtDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	webtest.Probe(web.Chain(web.Liveness(), middleware.RequestLogger(logger)), web.HealthPath)

	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	if got := out["level"]; got != "DEBUG" {
		t.Errorf("level = %v, want DEBUG", got)
	}
	if got := out["url.path"]; got != web.HealthPath {
		t.Errorf("path = %v, want %s", got, web.HealthPath)
	}
}

func TestRequestLogger_ProbeSuccessSilentAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	webtest.Probe(web.Chain(web.Liveness(), middleware.RequestLogger(logger)), web.HealthPath)

	if buf.Len() != 0 {
		t.Errorf("a successful probe logged through an info-level handler: %s", buf.String())
	}
}

// A failing probe is signal — readiness flapping must stay visible at info.
func TestRequestLogger_ProbeFailureLogsAtInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := web.Chain(
		web.Readiness(web.Problem{}, lifecycle.Check{Name: "lifecycle"}),
		middleware.RequestLogger(logger),
	)
	webtest.Probe(handler, web.ReadyPath)

	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	if got := out["level"]; got != "INFO" {
		t.Errorf("level = %v, want INFO", got)
	}
	if got := out["http.response.status_code"]; got != float64(http.StatusServiceUnavailable) {
		t.Errorf("status = %v, want 503", got)
	}
}

// The logger is not a recovery point. Without a Recoverer the panic
// continues to net/http, which drops the connection; the record says 500,
// the failure the client saw, and carries no panic value — that belongs to
// whichever recovery point catches it.
func TestRequestLogger_PanicWithoutRecovererPropagatesAndLogsAFailure(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := web.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}), middleware.RequestLogger(logger))

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		webtest.Probe(handler, "/orders/7")
	}()

	if recovered != "boom" {
		t.Fatalf("recovered %v, want the panic to propagate as boom", recovered)
	}

	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	for _, tc := range []struct {
		key  string
		want any
	}{
		{"msg", "request"},
		{"level", "INFO"},
		{"http.response.status_code", float64(http.StatusInternalServerError)},
		{"url.path", "/orders/7"},
	} {
		if got := out[tc.key]; got != tc.want {
			t.Errorf("%s = %v, want %v", tc.key, got, tc.want)
		}
	}
	if _, present := out["panic"]; present {
		t.Errorf("panic = %v, want the logger to leave the panic value to the recovery point", out["panic"])
	}
}

// A panic committed by the handler before it unwinds is still the status
// the client got, so the record keeps it regardless of the panic.
func TestRequestLogger_PanicAfterCommitKeepsTheCommittedStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("boom")
	}), middleware.RequestLogger(logger))

	func() {
		defer func() { _ = recover() }()
		webtest.Probe(handler, "/orders/7")
	}()

	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	if got := out["http.response.status_code"]; got != float64(http.StatusAccepted) {
		t.Errorf("status = %v, want the committed 202", got)
	}
}

// Recoverer and RequestLogger compose in either order. In Chain(h, a, b),
// a is outermost: its deferred work runs last, so both records into one
// buffer land in the order the middleware unwound, and that order pins
// which one was outermost. In both orders the client gets a 500 problem,
// the request record says 500, and the recoverer's record has the panic.
func TestRequestLogger_WithRecovererInEitherOrder(t *testing.T) {
	tests := []struct {
		name  string
		chain func(h http.Handler, recoverer, logger web.Middleware) http.Handler
		order []string // messages in emission order
	}{
		{
			"recoverer outermost",
			func(h http.Handler, recoverer, logger web.Middleware) http.Handler {
				return web.Chain(h, recoverer, logger)
			},
			[]string{"request", "handler panicked"},
		},
		{
			"recoverer innermost",
			func(h http.Handler, recoverer, logger web.Middleware) http.Handler {
				return web.Chain(h, logger, recoverer)
			},
			[]string{"handler panicked", "request"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			handler := tt.chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic("boom")
			}), middleware.Recoverer(logger), middleware.RequestLogger(logger))

			rec := webtest.Probe(handler, "/orders/7")

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
				t.Errorf("content type = %q, want %q", ct, web.ProblemMediaType)
			}

			logs := records(t, buf.String())
			if len(logs) != len(tt.order) {
				t.Fatalf("logged %d records, want %d: %v", len(logs), len(tt.order), logs)
			}
			byMsg := map[string]map[string]any{}
			for i, out := range logs {
				if out["msg"] != tt.order[i] {
					t.Errorf("record %d msg = %v, want %q", i, out["msg"], tt.order[i])
				}
				byMsg[out["msg"].(string)] = out
			}
			if got := byMsg["request"]["http.response.status_code"]; got != float64(http.StatusInternalServerError) {
				t.Errorf("request status = %v, want the 500 the client got", got)
			}
			if got := byMsg["handler panicked"]["panic"]; got != "boom" {
				t.Errorf("panic = %v, want boom", got)
			}
		})
	}
}

func TestRequestLogger_NilLoggerPanics(t *testing.T) {
	mustPanic(t, "RequestLogger(nil)", func() {
		middleware.RequestLogger(nil)
	})
}

func TestRequestLogger_HijackThroughWrapper(t *testing.T) {
	hijackErr := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, bw, err := http.NewResponseController(w).Hijack()
		hijackErr <- err
		if err != nil {
			return
		}
		_, _ = bw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
		_ = bw.Flush()
		_ = conn.Close()
	})

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ts := httptest.NewServer(web.Chain(handler, middleware.RequestLogger(logger)))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := <-hijackErr; err != nil {
		t.Errorf("Hijack through the wrapper: %v", err)
	}
}

// logged serves req through h and returns the one record the request logger
// wrote to buf.
func logged(t *testing.T, h http.Handler, buf *bytes.Buffer, req *http.Request) map[string]any {
	t.Helper()
	h.ServeHTTP(httptest.NewRecorder(), req)
	logs := records(t, buf.String())
	if len(logs) != 1 {
		t.Fatalf("logged %d records, want 1: %v", len(logs), logs)
	}
	return logs[0]
}

// withID returns a GET for path whose context carries id as the request id,
// as RequestID would have set it.
func withID(path, id string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	return req.WithContext(web.WithRequestID(req.Context(), id))
}

// http.route is the matched ServeMux pattern without its method token, for
// a module route and a native-mux route alike, whether the logger wraps the
// whole dispatch (Router.Use) or runs inside the route's own chain.
func TestRequestLogger_RouteIsTheMatchedPattern(t *testing.T) {
	tests := []struct {
		name  string
		build func(logger web.Middleware) http.Handler
		path  string
		route string
	}{
		{
			"module route under Router.Use",
			func(logger web.Middleware) http.Handler {
				g := web.NewGroup("/api")
				g.Handle(http.MethodGet, "/orders/{id}", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				r := web.NewRouter()
				r.Mount(web.NewModule(g))
				r.Use(logger)
				return r
			},
			"/api/orders/7",
			"/api/orders/{id}",
		},
		{
			"method-less native pattern",
			func(logger web.Middleware) http.Handler {
				r := web.NewRouter()
				r.Handle("/status", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				r.Use(logger)
				return r
			},
			"/status",
			"/status",
		},
		{
			"logger inside the route's chain",
			func(logger web.Middleware) http.Handler {
				g := web.NewGroup("/api")
				g.Use(logger)
				g.Handle(http.MethodGet, "/orders/{id}", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				r := web.NewRouter()
				r.Mount(web.NewModule(g))
				return r
			},
			"/api/orders/7",
			"/api/orders/{id}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := tt.build(middleware.RequestLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

			out := logged(t, h, &buf, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if got := out["http.route"]; got != tt.route {
				t.Errorf("http.route = %v, want %q", got, tt.route)
			}
			if got := out["url.path"]; got != tt.path {
				t.Errorf("url.path = %v, want %q", got, tt.path)
			}
		})
	}
}

// A request that matched no pattern has no route: a bare request that never
// went through a mux, and a router miss answered by the not-found handler.
// The attribute is omitted, not logged empty.
func TestRequestLogger_RouteOmittedWithoutAMatch(t *testing.T) {
	t.Run("bare request", func(t *testing.T) {
		out := record(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		if route, present := out["http.route"]; present {
			t.Errorf("http.route = %v, want it absent", route)
		}
	})
	t.Run("router miss", func(t *testing.T) {
		var buf bytes.Buffer
		r := web.NewRouter()
		r.Handle("GET /status", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		r.Use(middleware.RequestLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

		out := logged(t, r, &buf, httptest.NewRequest(http.MethodGet, "/nowhere", nil))

		if got := out["http.response.status_code"]; got != float64(http.StatusNotFound) {
			t.Errorf("status = %v, want 404", got)
		}
		if route, present := out["http.route"]; present {
			t.Errorf("http.route = %v, want it absent", route)
		}
	})
}

// ServeMux sets Pattern on the request it is handed, and RequestLogger reads
// it from the request it passed down. RequestID forwards a derived request,
// so with RequestID between the logger and the mux the logger's request
// never sees the pattern (or the id): http.route and request_id are both
// omitted. With RequestID outside the logger, both are present.
func TestRequestLogger_RouteAndIDNeedRequestIDOutside(t *testing.T) {
	tests := []struct {
		name    string
		chain   func(h http.Handler, logger, id web.Middleware) http.Handler
		present bool
	}{
		{
			"RequestID outside RequestLogger",
			func(h http.Handler, logger, id web.Middleware) http.Handler {
				return web.Chain(h, id, logger)
			},
			true,
		},
		{
			"RequestID between RequestLogger and the mux",
			func(h http.Handler, logger, id web.Middleware) http.Handler {
				return web.Chain(h, logger, id)
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := web.NewRouter()
			r.Handle("GET /status", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			logger := middleware.RequestLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
			h := tt.chain(r, logger, middleware.RequestID())

			out := logged(t, h, &buf, httptest.NewRequest(http.MethodGet, "/status", nil))

			for _, key := range []string{"http.route", "request_id"} {
				_, present := out[key]
				if present != tt.present {
					t.Errorf("%s present = %v, want %v (record %v)", key, present, tt.present, out)
				}
			}
			if tt.present && out["http.route"] != "/status" {
				t.Errorf("http.route = %v, want /status", out["http.route"])
			}
		})
	}
}

// request_id is the correlation id from the request's context, present only
// when one is set; a chain with no RequestID logs no empty id.
func TestRequestLogger_RequestIDFromContext(t *testing.T) {
	var buf bytes.Buffer
	h := web.Chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		middleware.RequestLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
	)

	out := logged(t, h, &buf, withID("/orders/7", "abc123"))
	if got := out["request_id"]; got != "abc123" {
		t.Errorf("request_id = %v, want abc123", got)
	}

	buf.Reset()
	out = logged(t, h, &buf, httptest.NewRequest(http.MethodGet, "/orders/7", nil))
	if id, present := out["request_id"]; present {
		t.Errorf("request_id = %v, want it absent", id)
	}
}
