package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

// decodeBody reads a recorded JSON response into a generic map so a test can
// assert on members the response type doesn't declare.
func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestProblem_WriteAppliesDefaults(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := (web.Problem{Status: http.StatusNotFound}).Write(rec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != web.ProblemMediaType {
		t.Errorf("Content-Type = %q, want %q", got, web.ProblemMediaType)
	}

	body := decodeBody(t, rec)
	if got := body["type"]; got != web.ProblemTypeBlank {
		t.Errorf("type = %v, want %q", got, web.ProblemTypeBlank)
	}
	if got := body["title"]; got != "Not Found" {
		t.Errorf("title = %v, want the status phrase", got)
	}
	if _, ok := body["detail"]; ok {
		t.Error("detail is present but was never set")
	}
}

func TestProblem_WriteKeepsConsumerType(t *testing.T) {
	// The library defines no problem types of its own; a consumer supplies its
	// own URI and it must survive untouched.
	const consumerType = "https://example.test/probs/out-of-credit"

	rec := httptest.NewRecorder()
	err := web.Problem{
		Type:   consumerType,
		Title:  "You do not have enough credit",
		Status: http.StatusForbidden,
	}.Write(rec)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	body := decodeBody(t, rec)
	if got := body["type"]; got != consumerType {
		t.Errorf("type = %v, want %q", got, consumerType)
	}
	if got := body["title"]; got != "You do not have enough credit" {
		t.Errorf("title = %v, want the supplied title", got)
	}
}

func TestWriteProblem_UsesRequestPathAsInstance(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/orders/42", nil)

	err := web.WriteProblem(rec, r, http.StatusConflict, "", "order already shipped")
	if err != nil {
		t.Fatalf("WriteProblem: %v", err)
	}

	body := decodeBody(t, rec)
	if got := body["instance"]; got != "/orders/42" {
		t.Errorf("instance = %v, want /orders/42", got)
	}
	if got := body["title"]; got != "Conflict" {
		t.Errorf("title = %v, want the status phrase", got)
	}
	if got := body["detail"]; got != "order already shipped" {
		t.Errorf("detail = %v, want the supplied detail", got)
	}
}

func TestWriteProblemWith_CarriesExtensionMembers(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	err := web.WriteProblemWith(
		rec, r,
		http.StatusServiceUnavailable,
		"",
		"one or more readiness checks failed",
		map[string]any{"checks": []map[string]any{{"name": "database", "ready": false}}},
	)
	if err != nil {
		t.Fatalf("WriteProblemWith: %v", err)
	}

	body := decodeBody(t, rec)
	if got := body["title"]; got != "Service Unavailable" {
		t.Errorf("title = %v, want the status phrase", got)
	}
	if got := body["status"]; got != float64(http.StatusServiceUnavailable) {
		t.Errorf("status = %v, want 503", got)
	}
	checks, ok := body["checks"].([]any)
	if !ok || len(checks) != 1 {
		t.Fatalf("checks = %v, want one entry", body["checks"])
	}
}

func TestWriteProblemWith_ExtrasOverrideStandardMembers(t *testing.T) {
	const consumerType = "https://example.test/probs/not-ready"

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	err := web.WriteProblemWith(
		rec, r,
		http.StatusServiceUnavailable,
		"",
		"",
		map[string]any{"type": consumerType},
	)
	if err != nil {
		t.Fatalf("WriteProblemWith: %v", err)
	}

	body := decodeBody(t, rec)
	if got := body["type"]; got != consumerType {
		t.Errorf("type = %v, want the override %q", got, consumerType)
	}
	if _, ok := body["detail"]; ok {
		t.Error("detail is present but was empty")
	}
}

func TestProblem_WriteZeroStatusDefaultsToInternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := (web.Problem{Detail: "x"}).Write(rec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	body := decodeBody(t, rec)
	if got := body["status"]; got != float64(http.StatusInternalServerError) {
		t.Errorf("body status = %v, want 500", got)
	}
	if got := body["title"]; got != "Internal Server Error" {
		t.Errorf("title = %v, want Internal Server Error", got)
	}
	if got := body["detail"]; got != "x" {
		t.Errorf("detail = %v, want x", got)
	}
}

func TestProblem_WriteUnknownStatusInventsNoTitle(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := (web.Problem{Status: 599}).Write(rec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if rec.Code != 599 {
		t.Errorf("status = %d, want 599", rec.Code)
	}
	body := decodeBody(t, rec)
	if got, ok := body["title"]; ok {
		t.Errorf("title = %v, want the member omitted for a code with no standard phrase", got)
	}
}

func TestProblem_WriteDefaultsTitleForConsumerType(t *testing.T) {
	const consumerType = "https://example.com/e"

	rec := httptest.NewRecorder()
	err := web.Problem{Type: consumerType, Status: http.StatusNotFound}.Write(rec)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	body := decodeBody(t, rec)
	if got := body["type"]; got != consumerType {
		t.Errorf("type = %v, want %q", got, consumerType)
	}
	if got := body["title"]; got != "Not Found" {
		t.Errorf("title = %v, want the status phrase", got)
	}
}

func TestWriteProblemWith_ExtrasCannotDesyncStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	err := web.WriteProblemWith(
		rec, r,
		http.StatusServiceUnavailable,
		"",
		"",
		map[string]any{"status": 200, "trace": "abc"},
	)
	if err != nil {
		t.Fatalf("WriteProblemWith: %v", err)
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	body := decodeBody(t, rec)
	if got := body["status"]; got != float64(http.StatusServiceUnavailable) {
		t.Errorf("body status = %v, want 503 despite the extras override", got)
	}
	if got := body["trace"]; got != "abc" {
		t.Errorf("trace = %v, want abc", got)
	}
}

func TestWriteProblemWith_UnknownStatusOmitsTitle(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)

	if err := web.WriteProblemWith(rec, r, 599, "", "", nil); err != nil {
		t.Fatalf("WriteProblemWith: %v", err)
	}

	body := decodeBody(t, rec)
	if got, ok := body["title"]; ok {
		t.Errorf("title = %v, want the member omitted for a code with no standard phrase", got)
	}
}
