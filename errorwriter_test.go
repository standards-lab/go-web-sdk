package web_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

// matcherFor is the consumer-matcher shape: claim errors matching a sentinel,
// pass on everything else.
func matcherFor(sentinel error, status int) web.StatusMatcher {
	return func(err error) (int, bool) {
		if errors.Is(err, sentinel) {
			return status, true
		}
		return 0, false
	}
}

func TestErrorWriter_QueryErrorIsBuiltIn(t *testing.T) {
	ew := web.NewErrorWriter()

	err := fmt.Errorf("listing: %w", &web.QueryError{
		Param: "page", Value: "0", Reason: "must be an integer of at least 1",
	})
	if got := ew.Status(err); got != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestErrorWriter_PreconditionErrorIsBuiltIn(t *testing.T) {
	ew := web.NewErrorWriter()

	missing := fmt.Errorf("edit: %w", &web.PreconditionError{Missing: true})
	if got := ew.Status(missing); got != http.StatusPreconditionRequired {
		t.Errorf("Status(missing) = %d, want %d", got, http.StatusPreconditionRequired)
	}
	malformed := fmt.Errorf("edit: %w", &web.PreconditionError{Value: `W/"3"`})
	if got := ew.Status(malformed); got != http.StatusBadRequest {
		t.Errorf("Status(malformed) = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestErrorWriter_BuiltInWinsOverMatchers(t *testing.T) {
	claimAll := func(error) (int, bool) { return http.StatusTeapot, true }
	ew := web.NewErrorWriter(claimAll)

	builtIn := map[string]struct {
		err  error
		want int
	}{
		"query":                {&web.QueryError{Param: "size", Value: "many", Reason: "must be an integer of at least 1"}, http.StatusBadRequest},
		"precondition missing": {&web.PreconditionError{Missing: true}, http.StatusPreconditionRequired},
		"precondition value":   {&web.PreconditionError{Value: "3"}, http.StatusBadRequest},
		"body too large":       {&web.BodyError{TooLarge: true, Reason: "exceeds the 16-byte limit"}, http.StatusRequestEntityTooLarge},
		"body rejected":        {&web.BodyError{Reason: "unknown field"}, http.StatusBadRequest},
	}
	for name, tt := range builtIn {
		if got := ew.Status(tt.err); got != tt.want {
			t.Errorf("Status(%s) = %d, want %d", name, got, tt.want)
		}
	}
}

func TestErrorWriter_Write428CarriesTheErrorText(t *testing.T) {
	ew := web.NewErrorWriter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/things/1", nil)

	perr := &web.PreconditionError{Missing: true}
	if err := ew.Write(rec, req, perr); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if rec.Code != http.StatusPreconditionRequired {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusPreconditionRequired)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body["detail"] != perr.Error() {
		t.Errorf("detail = %q, want %q", body["detail"], perr.Error())
	}
}

func TestErrorWriter_FirstMatchWins(t *testing.T) {
	sentinel := errors.New("version mismatch")
	ew := web.NewErrorWriter(
		matcherFor(sentinel, http.StatusPreconditionFailed),
		matcherFor(sentinel, http.StatusConflict),
	)

	err := fmt.Errorf("update: %w", sentinel)
	if got := ew.Status(err); got != http.StatusPreconditionFailed {
		t.Errorf("Status = %d, want %d", got, http.StatusPreconditionFailed)
	}
}

func TestErrorWriter_UnclaimedErrorIs500(t *testing.T) {
	ew := web.NewErrorWriter(matcherFor(errors.New("unrelated"), http.StatusConflict))

	if got := ew.Status(errors.New("driver: connection reset")); got != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", got, http.StatusInternalServerError)
	}
}

func TestErrorWriter_Write400CarriesTheErrorText(t *testing.T) {
	ew := web.NewErrorWriter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/things?page=0", nil)

	qerr := &web.QueryError{Param: "page", Value: "0", Reason: "must be an integer of at least 1"}
	if err := ew.Write(rec, req, qerr); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
		t.Errorf("content type = %q, want %q", ct, web.ProblemMediaType)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body["detail"] != qerr.Error() {
		t.Errorf("detail = %q, want %q", body["detail"], qerr.Error())
	}
	if body["instance"] != "/things" {
		t.Errorf("instance = %q, want %q", body["instance"], "/things")
	}
}

func TestErrorWriter_DetailAddsStatusesThatCarryTheErrorText(t *testing.T) {
	conflict := errors.New("schema is dirty at version 7")
	forbidden := errors.New("seeding is disabled in this environment")
	internal := errors.New("driver: connection reset")
	ew := web.NewErrorWriter(
		matcherFor(conflict, http.StatusConflict),
		matcherFor(forbidden, http.StatusForbidden),
	)
	ew.Detail(http.StatusConflict, http.StatusInternalServerError)

	tests := []struct {
		name   string
		err    error
		status int
		detail bool
	}{
		{"added conflict carries text", conflict, http.StatusConflict, true},
		{"unadded forbidden stays bare", forbidden, http.StatusForbidden, false},
		{"consumer's choice is honored even on 500", internal, http.StatusInternalServerError, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/admin/schema/up", nil)

			if err := ew.Write(rec, req, tt.err); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			detail, present := body["detail"]
			if present != tt.detail {
				t.Errorf("detail present = %v, want %v", present, tt.detail)
			}
			if tt.detail && detail != tt.err.Error() {
				t.Errorf("detail = %q, want %q", detail, tt.err.Error())
			}
		})
	}
}

func TestErrorWriter_WriteAboveA400SendsNoErrorText(t *testing.T) {
	sentinel := errors.New("unique constraint violation")
	ew := web.NewErrorWriter(matcherFor(sentinel, http.StatusConflict))

	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"matched conflict", fmt.Errorf("insert: %w", sentinel), http.StatusConflict},
		{"unclaimed internal", errors.New("driver: connection reset"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/things", nil)

			if err := ew.Write(rec, req, tt.err); err != nil {
				t.Fatalf("Write: %v", err)
			}

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if _, ok := body["detail"]; ok {
				t.Errorf("detail = %q, want it absent", body["detail"])
			}
			if body["title"] != http.StatusText(tt.status) {
				t.Errorf("title = %q, want %q", body["title"], http.StatusText(tt.status))
			}
		})
	}
}
