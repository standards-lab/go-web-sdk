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
		{"method", http.MethodGet},
		{"path", "/orders/7"},
		{"status", float64(http.StatusCreated)},
		{"remote_addr", "192.0.2.1:1234"},
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

	if got := out["status"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", got)
	}
}

func TestRequestLogger_RecordsProblemStatus(t *testing.T) {
	out := record(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = web.WriteProblem(w, r, http.StatusTeapot, "", "no coffee")
	}))

	if got := out["status"]; got != float64(http.StatusTeapot) {
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

	if got := out["status"]; got != float64(http.StatusAccepted) {
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
	if got := out["status"]; got != float64(http.StatusOK) {
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
	if got := out["path"]; got != web.HealthPath {
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
		web.Readiness(lifecycle.Check{Name: "lifecycle"}),
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
	if got := out["status"]; got != float64(http.StatusServiceUnavailable) {
		t.Errorf("status = %v, want 503", got)
	}
}

func TestRequestLogger_PanicLogsAtErrorAndRepanics(t *testing.T) {
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
		{"level", "ERROR"},
		{"panic", "boom"},
		{"path", "/orders/7"},
	} {
		if got := out[tc.key]; got != tc.want {
			t.Errorf("%s = %v, want %v", tc.key, got, tc.want)
		}
	}
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
