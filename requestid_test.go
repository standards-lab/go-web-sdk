package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

// requestWithID builds a GET request for path whose context carries id.
func requestWithID(path, id string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	return r.WithContext(web.WithRequestID(r.Context(), id))
}

func TestWithRequestID_RoundTrips(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), "req-1")

	got, ok := web.RequestIDFrom(ctx)
	if !ok || got != "req-1" {
		t.Errorf("RequestIDFrom = %q, %v; want req-1, true", got, ok)
	}
}

func TestWithRequestID_LaterCallReplacesEarlier(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), "first")
	ctx = web.WithRequestID(ctx, "second")

	got, ok := web.RequestIDFrom(ctx)
	if !ok || got != "second" {
		t.Errorf("RequestIDFrom = %q, %v; want second, true", got, ok)
	}
}

func TestRequestIDFrom_UnsetReportsFalse(t *testing.T) {
	got, ok := web.RequestIDFrom(context.Background())
	if ok || got != "" {
		t.Errorf("RequestIDFrom = %q, %v; want \"\", false", got, ok)
	}
}

func TestProblem_WriteForCarriesRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	r := requestWithID("/orders/42", "req-42")

	err := web.Problem{Status: http.StatusNotFound}.WriteFor(rec, r)
	if err != nil {
		t.Fatalf("WriteFor: %v", err)
	}

	body := decodeBody(t, rec)
	if got := body["request_id"]; got != "req-42" {
		t.Errorf("request_id = %v, want req-42", got)
	}
	// The id lives in its own member; instance stays the request path.
	if got := body["instance"]; got != "/orders/42" {
		t.Errorf("instance = %v, want /orders/42", got)
	}
}

func TestProblem_WriteForCarriesRequestIDBesideExtras(t *testing.T) {
	rec := httptest.NewRecorder()
	r := requestWithID("/readyz", "req-7")

	err := web.Problem{
		Status: http.StatusServiceUnavailable,
		Extras: map[string]any{"checks": []map[string]any{{"name": "database", "ready": false}}},
	}.WriteFor(rec, r)
	if err != nil {
		t.Fatalf("WriteFor: %v", err)
	}

	body := decodeBody(t, rec)
	if got := body["request_id"]; got != "req-7" {
		t.Errorf("request_id = %v, want req-7", got)
	}
	if checks, ok := body["checks"].([]any); !ok || len(checks) != 1 {
		t.Errorf("checks = %v, want the caller's member preserved beside the id", body["checks"])
	}
}

// A consumer with no correlation id configured must see the document shape it
// had before: no request_id member at all, not an empty one.
func TestProblem_WriteForWithoutRequestIDOmitsMember(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/orders/42", nil)

	err := web.Problem{Status: http.StatusNotFound}.WriteFor(rec, r)
	if err != nil {
		t.Fatalf("WriteFor: %v", err)
	}

	body := decodeBody(t, rec)
	if got, ok := body["request_id"]; ok {
		t.Errorf("request_id = %v, want the member absent", got)
	}
}

func TestProblem_WriteForEmptyRequestIDOmitsMember(t *testing.T) {
	rec := httptest.NewRecorder()
	r := requestWithID("/orders/42", "")

	err := web.Problem{Status: http.StatusNotFound}.WriteFor(rec, r)
	if err != nil {
		t.Fatalf("WriteFor: %v", err)
	}

	body := decodeBody(t, rec)
	if got, ok := body["request_id"]; ok {
		t.Errorf("request_id = %v, want the member absent for an empty id", got)
	}
}

// The id from the context is the SDK's assertion of which request the
// document answers, so it wins over a request_id the caller put in Extras.
func TestProblem_WriteForContextRequestIDOverridesExtras(t *testing.T) {
	rec := httptest.NewRecorder()
	r := requestWithID("/orders/42", "from-context")

	err := web.Problem{
		Status: http.StatusNotFound,
		Extras: map[string]any{"request_id": "from-caller"},
	}.WriteFor(rec, r)
	if err != nil {
		t.Fatalf("WriteFor: %v", err)
	}

	body := decodeBody(t, rec)
	if got := body["request_id"]; got != "from-context" {
		t.Errorf("request_id = %v, want from-context", got)
	}
}

// A Problem value is reused across requests (a matcher's fixed problem, the
// readiness notReady), so merging the id must not write the caller's map.
func TestProblem_WriteForDoesNotMutateExtras(t *testing.T) {
	extras := map[string]any{"balance": 0}
	p := web.Problem{Status: http.StatusForbidden, Extras: extras}

	for _, id := range []string{"req-a", "req-b"} {
		rec := httptest.NewRecorder()
		if err := p.WriteFor(rec, requestWithID("/accounts/1", id)); err != nil {
			t.Fatalf("WriteFor(%s): %v", id, err)
		}
		if got := decodeBody(t, rec)["request_id"]; got != id {
			t.Errorf("request_id = %v, want %s", got, id)
		}
	}

	if len(extras) != 1 {
		t.Errorf("caller's Extras = %v, want the one original member", extras)
	}
	if _, ok := extras["request_id"]; ok {
		t.Error("caller's Extras gained request_id")
	}
	if len(p.Extras) != 1 {
		t.Errorf("p.Extras = %v, want the original map unchanged", p.Extras)
	}
}
