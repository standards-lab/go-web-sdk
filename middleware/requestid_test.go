package middleware_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// generatedID matches the shape RequestID generates: 16 bytes as 32
// lowercase hex characters.
var generatedID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// identified serves one GET through RequestID built with opts, with inbound
// carrying any request headers to send, and returns the recorder and the id
// the downstream handler read from its context.
func identified(t *testing.T, inbound http.Header, opts ...middleware.RequestIDOption) (*httptest.ResponseRecorder, string) {
	t.Helper()

	var seen string
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := web.RequestIDFrom(r.Context())
		if !ok {
			t.Error("RequestIDFrom = false inside the handler, want the id set")
		}
		seen = id
		w.WriteHeader(http.StatusNoContent)
	}), middleware.RequestID(opts...))

	r := httptest.NewRequest(http.MethodGet, "/orders/7", nil)
	for name, values := range inbound {
		r.Header[name] = values
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	return rec, seen
}

// fixedSource is an id source that returns id for every request.
func fixedSource(id string) func(*http.Request) string {
	return func(*http.Request) string { return id }
}

func TestRequestID_GeneratesByDefault(t *testing.T) {
	rec, seen := identified(t, nil)

	got := rec.Header().Get(middleware.RequestIDHeader)
	if !generatedID.MatchString(got) {
		t.Errorf("%s = %q, want 32 lowercase hex characters", middleware.RequestIDHeader, got)
	}
	if seen != got {
		t.Errorf("context id = %q, want the response header's %q", seen, got)
	}
}

func TestRequestID_GeneratedIDsDiffer(t *testing.T) {
	_, first := identified(t, nil)
	_, second := identified(t, nil)

	if first == second {
		t.Errorf("two requests got the same generated id %q", first)
	}
}

// An inbound X-Request-Id is not echoed unless the caller opted in through
// WithTrustedHeader: any client can send one.
func TestRequestID_IgnoresAnUntrustedInboundHeader(t *testing.T) {
	rec, seen := identified(t, http.Header{middleware.RequestIDHeader: {"client-chosen"}})

	if seen == "client-chosen" || rec.Header().Get(middleware.RequestIDHeader) == "client-chosen" {
		t.Errorf("echoed the inbound id without WithTrustedHeader: context %q, header %q",
			seen, rec.Header().Get(middleware.RequestIDHeader))
	}
	if !generatedID.MatchString(seen) {
		t.Errorf("id = %q, want a generated one", seen)
	}
}

func TestRequestID_EchoesATrustedHeader(t *testing.T) {
	rec, seen := identified(t,
		http.Header{"X-Proxy-Id": {"proxy-42"}},
		middleware.WithTrustedHeader("x-proxy-id"),
	)

	if seen != "proxy-42" {
		t.Errorf("context id = %q, want the trusted header's proxy-42", seen)
	}
	if got := rec.Header().Get(middleware.RequestIDHeader); got != "proxy-42" {
		t.Errorf("%s = %q, want proxy-42", middleware.RequestIDHeader, got)
	}
}

func TestRequestID_TrustedHeaderMissingFallsThroughToGeneration(t *testing.T) {
	tests := []struct {
		name    string
		inbound http.Header
	}{
		{"absent", nil},
		{"empty", http.Header{"X-Proxy-Id": {""}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, seen := identified(t, tt.inbound, middleware.WithTrustedHeader("X-Proxy-Id"))

			if !generatedID.MatchString(seen) {
				t.Errorf("context id = %q, want a generated one", seen)
			}
			if got := rec.Header().Get(middleware.RequestIDHeader); got != seen {
				t.Errorf("%s = %q, want the context's %q", middleware.RequestIDHeader, got, seen)
			}
		})
	}
}

func TestRequestID_SourceIsUsedVerbatim(t *testing.T) {
	rec, seen := identified(t, nil, middleware.WithIDSource(fixedSource("Trace-ABC")))

	if seen != "Trace-ABC" {
		t.Errorf("context id = %q, want the source's Trace-ABC untouched", seen)
	}
	if got := rec.Header().Get(middleware.RequestIDHeader); got != "Trace-ABC" {
		t.Errorf("%s = %q, want Trace-ABC", middleware.RequestIDHeader, got)
	}
}

func TestRequestID_SourceSeesTheRequest(t *testing.T) {
	_, seen := identified(t, nil, middleware.WithIDSource(func(r *http.Request) string {
		return r.Method + " " + r.URL.Path
	}))

	if seen != "GET /orders/7" {
		t.Errorf("context id = %q, want the source to have seen the request", seen)
	}
}

// The precedence chain: the source first, the trusted header second,
// generation last. Each step is skipped when it yields nothing.
func TestRequestID_Precedence(t *testing.T) {
	inbound := http.Header{"X-Proxy-Id": {"proxy-42"}}
	tests := []struct {
		name    string
		inbound http.Header
		opts    []middleware.RequestIDOption
		want    string // "" means a generated id
	}{
		{
			"source wins over a present trusted header",
			inbound,
			[]middleware.RequestIDOption{
				middleware.WithIDSource(fixedSource("trace-1")),
				middleware.WithTrustedHeader("X-Proxy-Id"),
			},
			"trace-1",
		},
		{
			"source wins in either option order",
			inbound,
			[]middleware.RequestIDOption{
				middleware.WithTrustedHeader("X-Proxy-Id"),
				middleware.WithIDSource(fixedSource("trace-1")),
			},
			"trace-1",
		},
		{
			"a declining source yields to a present trusted header",
			inbound,
			[]middleware.RequestIDOption{
				middleware.WithIDSource(fixedSource("")),
				middleware.WithTrustedHeader("X-Proxy-Id"),
			},
			"proxy-42",
		},
		{
			"a declining source and an absent trusted header generate",
			nil,
			[]middleware.RequestIDOption{
				middleware.WithIDSource(fixedSource("")),
				middleware.WithTrustedHeader("X-Proxy-Id"),
			},
			"",
		},
		{
			"a declining source alone generates",
			nil,
			[]middleware.RequestIDOption{middleware.WithIDSource(fixedSource(""))},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, seen := identified(t, tt.inbound, tt.opts...)

			switch {
			case tt.want == "" && !generatedID.MatchString(seen):
				t.Errorf("context id = %q, want a generated one", seen)
			case tt.want != "" && seen != tt.want:
				t.Errorf("context id = %q, want %q", seen, tt.want)
			}
			if got := rec.Header().Get(middleware.RequestIDHeader); got != seen {
				t.Errorf("%s = %q, want the context's %q", middleware.RequestIDHeader, got, seen)
			}
		})
	}
}

// A problem document written downstream carries the id as its request_id
// member, the same value the response header shows.
func TestRequestID_ProblemDocumentCarriesTheID(t *testing.T) {
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = web.Problem{Status: http.StatusNotFound}.WriteFor(w, r)
	}), middleware.RequestID())

	rec := webtest.Probe(handler, "/orders/7")

	var problem map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	header := rec.Header().Get(middleware.RequestIDHeader)
	if header == "" {
		t.Fatalf("%s is unset", middleware.RequestIDHeader)
	}
	if problem["request_id"] != header {
		t.Errorf("request_id = %v, want the response header's %q", problem["request_id"], header)
	}
}

// The response header is set before the handler runs, so it is on the wire
// whoever writes the response: here a Recoverer answering a panic, in
// either chain order. The context id is a different matter: a middleware
// outside RequestID holds the original request, whose context never got
// the id, so the recovered 500's problem document carries request_id only
// when RequestID is the outer of the two.
func TestRequestID_HeaderSurvivesARecoveredPanic(t *testing.T) {
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	tests := []struct {
		name      string
		handler   http.Handler
		inProblem bool
	}{
		{
			"RequestID outside Recoverer",
			web.Chain(panicking, middleware.RequestID(), middleware.Recoverer(logger)),
			true,
		},
		{
			"Recoverer outside RequestID",
			web.Chain(panicking, middleware.Recoverer(logger), middleware.RequestID()),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := webtest.Probe(tt.handler, "/orders/7")

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			header := rec.Header().Get(middleware.RequestIDHeader)
			if !generatedID.MatchString(header) {
				t.Errorf("%s = %q, want a generated id on the recovered response", middleware.RequestIDHeader, header)
			}
			var problem map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			got, present := problem["request_id"]
			switch {
			case tt.inProblem && got != header:
				t.Errorf("request_id = %v, want the response header's %q", got, header)
			case !tt.inProblem && present:
				t.Errorf("request_id = %v, want it absent: the Recoverer's request predates RequestID", got)
			}
		})
	}
}

// RequestID does not wrap the writer, so a handler's body reaches the
// client as written.
func TestRequestID_PassesTheResponseThrough(t *testing.T) {
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = web.WriteJSON(w, http.StatusCreated, map[string]string{"id": "7"})
	}), middleware.RequestID())

	rec := webtest.Probe(handler, "/orders/7")

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if got := bytes.TrimSpace(rec.Body.Bytes()); string(got) != `{"id":"7"}` {
		t.Errorf("body = %q, want %q", got, `{"id":"7"}`)
	}
}

func TestWithIDSource_NilPanics(t *testing.T) {
	mustPanic(t, "WithIDSource(nil)", func() {
		middleware.WithIDSource(nil)
	})
}

func TestWithTrustedHeader_EmptyPanics(t *testing.T) {
	mustPanic(t, `WithTrustedHeader("")`, func() {
		middleware.WithTrustedHeader("")
	})
}
