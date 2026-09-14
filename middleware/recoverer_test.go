package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// mustPanic runs fn and fails the test unless it panics.
func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", name)
		}
	}()
	fn()
}

// recovered serves one GET through the recoverer and returns the response
// and whatever the recoverer logged, one map per JSON line.
func recovered(t *testing.T, handler http.Handler) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	rec := webtest.Probe(web.Chain(handler, middleware.Recoverer(logger)), "/orders/7")
	return rec, records(t, buf.String())
}

// records parses newline-delimited JSON log output.
func records(t *testing.T, out string) []map[string]any {
	t.Helper()

	var all []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		all = append(all, m)
	}
	return all
}

func TestRecoverer_PanicWithNothingWrittenIsAProblem(t *testing.T) {
	rec, logs := recovered(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
		t.Errorf("content type = %q, want %q", ct, web.ProblemMediaType)
	}
	var problem map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if problem["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("problem status = %v, want 500", problem["status"])
	}
	if problem["title"] != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("title = %q, want the status text", problem["title"])
	}
	if problem["instance"] != "/orders/7" {
		t.Errorf("instance = %q, want /orders/7", problem["instance"])
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("the panic value leaked into the response: %s", rec.Body.String())
	}

	if len(logs) != 1 {
		t.Fatalf("logged %d records, want 1: %v", len(logs), logs)
	}
	out := logs[0]
	for _, tc := range []struct {
		key  string
		want any
	}{
		{"msg", "handler panicked"},
		{"level", "ERROR"},
		{"panic", "boom"},
		{"method", http.MethodGet},
		{"path", "/orders/7"},
		{"remote_addr", "192.0.2.1:1234"},
	} {
		if got := out[tc.key]; got != tc.want {
			t.Errorf("%s = %v, want %v", tc.key, got, tc.want)
		}
	}
	if stack, _ := out["stack"].(string); !strings.Contains(stack, "recoverer_test.go") {
		t.Errorf("stack = %q, want the panicking frame", stack)
	}
	if _, present := out["status"]; present {
		t.Errorf("status = %v, want it absent when nothing was committed", out["status"])
	}
}

func TestRecoverer_LeavesANonPanickingRequestAlone(t *testing.T) {
	rec, logs := recovered(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = web.WriteJSON(w, http.StatusCreated, map[string]string{"id": "7"})
	}))

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"id":"7"}` {
		t.Errorf("body = %q, want %q", got, `{"id":"7"}`)
	}
	if len(logs) != 0 {
		t.Errorf("logged %v for a request that did not panic", logs)
	}
}

// A response already committed before the panic cannot be answered with a
// problem document, so Recoverer logs the committed status and re-raises
// the panic as http.ErrAbortHandler instead of returning normally: net/http
// ends the connection rather than completing a truncated body as if it
// were a clean response.
func TestRecoverer_PanicAfterCommitReRaisesErrAbortHandler(t *testing.T) {
	tests := []struct {
		name   string
		commit func(http.ResponseWriter)
		status int
	}{
		{"after WriteHeader", func(w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted) }, http.StatusAccepted},
		{"after an implicit 200 Write", func(w http.ResponseWriter) { _, _ = w.Write([]byte("partial")) }, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tt.commit(w)
				panic("boom")
			}), middleware.Recoverer(logger))

			var got any
			func() {
				defer func() { got = recover() }()
				webtest.Probe(handler, "/orders/7")
			}()

			if got != http.ErrAbortHandler {
				t.Errorf("recovered %v, want http.ErrAbortHandler to propagate", got)
			}

			logs := records(t, buf.String())
			if len(logs) != 1 {
				t.Fatalf("logged %d records, want 1: %v", len(logs), logs)
			}
			if logs[0]["panic"] != "boom" || logs[0]["level"] != "ERROR" {
				t.Errorf("record = %v, want the panic at error level", logs[0])
			}
			if logs[0]["status"] != float64(tt.status) {
				t.Errorf("status attr = %v, want the committed %d", logs[0]["status"], tt.status)
			}
		})
	}
}

func TestRecoverer_NilLoggerPanics(t *testing.T) {
	mustPanic(t, "Recoverer(nil)", func() {
		middleware.Recoverer(nil)
	})
}

// http.ErrAbortHandler is net/http's own way to abort a response silently;
// the recoverer passes it through rather than answering it with a problem.
func TestRecoverer_ErrAbortHandlerPropagates(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := web.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}), middleware.Recoverer(logger))

	var got any
	func() {
		defer func() { got = recover() }()
		webtest.Probe(handler, "/")
	}()

	if got != http.ErrAbortHandler {
		t.Errorf("recovered %v, want http.ErrAbortHandler to propagate", got)
	}
	if buf.Len() != 0 {
		t.Errorf("logged an abort: %s", buf.String())
	}
}
