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
	"github.com/standards-lab/go-web-sdk/internal/handlertest"
	"github.com/standards-lab/go-web-sdk/middleware"
)

func record(t *testing.T, handler http.Handler) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handlertest.Get(web.Chain(handler, middleware.RequestLogger(logger)), "/orders/7")

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

func TestRequestLogger_ImplicitStatusIsOK(t *testing.T) {
	out := record(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	if got := out["http.response.status_code"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", got)
	}
}

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
		_ = web.Problem{Status: http.StatusTeapot, Detail: "no coffee"}.WriteFor(w, r)
	}))

	if got := out["http.response.status_code"]; got != float64(http.StatusTeapot) {
		t.Errorf("status = %v, want 418", got)
	}
}

func TestRequestLogger_FirstStatusWins(t *testing.T) {
	out := record(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	if got := out["http.response.status_code"]; got != float64(http.StatusAccepted) {
		t.Errorf("status = %v, want 202", got)
	}
}

func TestRequestLogger_ResponseControllerReachesTheWriter(t *testing.T) {
	var flushErr error
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chunk"))
		flushErr = http.NewResponseController(w).Flush()
	})

	rec := handlertest.Get(web.Chain(handler, middleware.RequestLogger(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))), "/")

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
	rec := handlertest.Get(web.Chain(handler, middleware.RequestLogger(logger)), "/")

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

func TestRequestLogger_ProbeSuccessLogsAtDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handlertest.Get(web.Chain(web.Liveness(), middleware.RequestLogger(logger)), web.HealthPath)

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
	handlertest.Get(web.Chain(web.Liveness(), middleware.RequestLogger(logger)), web.HealthPath)

	if buf.Len() != 0 {
		t.Errorf("a successful probe logged through an info-level handler: %s", buf.String())
	}
}

func TestRequestLogger_ProbeFailureLogsAtInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := web.Chain(
		web.Readiness(web.Problem{}, lifecycle.Check{Name: "lifecycle"}),
		middleware.RequestLogger(logger),
	)
	handlertest.Get(handler, web.ReadyPath)

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

func TestRequestLogger_PanicWithoutRecovererPropagatesAndLogsAFailure(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := web.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}), middleware.RequestLogger(logger))

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handlertest.Get(handler, "/orders/7")
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

func TestRequestLogger_PanicAfterCommitKeepsTheCommittedStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("boom")
	}), middleware.RequestLogger(logger))

	func() {
		defer func() { _ = recover() }()
		handlertest.Get(handler, "/orders/7")
	}()

	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	if got := out["http.response.status_code"]; got != float64(http.StatusAccepted) {
		t.Errorf("status = %v, want the committed 202", got)
	}
}

func TestRequestLogger_WithRecovererInEitherOrder(t *testing.T) {
	tests := []struct {
		name  string
		chain func(h http.Handler, recoverer, logger web.Middleware) http.Handler
		order []string
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

			rec := handlertest.Get(handler, "/orders/7")

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

func logged(t *testing.T, h http.Handler, buf *bytes.Buffer, req *http.Request) map[string]any {
	t.Helper()
	h.ServeHTTP(httptest.NewRecorder(), req)
	logs := records(t, buf.String())
	if len(logs) != 1 {
		t.Fatalf("logged %d records, want 1: %v", len(logs), logs)
	}
	return logs[0]
}

func withID(path, id string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	return req.WithContext(web.WithRequestID(req.Context(), id))
}

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
			h := tt.chain(r, logger, middleware.RequestID(middleware.RequestIDOptions{}))

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
