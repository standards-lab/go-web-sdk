package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

// discard is the logger a test that does not read the writer's records
// wires it with.
var discard = slog.New(slog.DiscardHandler)

// matcherFor is the consumer-matcher shape: claim errors matching a sentinel
// with a status-only Problem, pass on everything else.
func matcherFor(sentinel error, status int) web.ProblemMatcher {
	return func(err error) (web.Problem, bool) {
		if errors.Is(err, sentinel) {
			return web.Problem{Status: status}, true
		}
		return web.Problem{}, false
	}
}

func TestNewErrorWriter_NilLoggerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != "web: NewErrorWriter requires a *slog.Logger" {
			t.Fatalf("panic = %v, want the wiring message", r)
		}
	}()
	web.NewErrorWriter(nil)
}

func TestErrorWriter_QueryErrorIsBuiltIn(t *testing.T) {
	ew := web.NewErrorWriter(discard)

	err := fmt.Errorf("listing: %w", &web.QueryError{
		Param: "page", Value: "0", Reason: "must be an integer of at least 1",
	})
	if got := ew.Problem(err).Status; got != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestErrorWriter_PreconditionErrorIsBuiltIn(t *testing.T) {
	ew := web.NewErrorWriter(discard)

	missing := fmt.Errorf("edit: %w", &web.PreconditionError{Missing: true})
	if got := ew.Problem(missing).Status; got != http.StatusPreconditionRequired {
		t.Errorf("Status(missing) = %d, want %d", got, http.StatusPreconditionRequired)
	}
	malformed := fmt.Errorf("edit: %w", &web.PreconditionError{Value: `W/"3"`})
	if got := ew.Problem(malformed).Status; got != http.StatusBadRequest {
		t.Errorf("Status(malformed) = %d, want %d", got, http.StatusBadRequest)
	}
}

// A handler that returns a Problem has decided the response itself: it is
// sent as is, members and all, rather than as an unclaimed 500, and no
// error text is added to it.
func TestErrorWriter_ReturnedProblemIsSentAsIs(t *testing.T) {
	ew := web.NewErrorWriter(discard)
	returned := web.Problem{
		Type:   "https://example.test/problems/quota",
		Status: http.StatusConflict,
		Title:  "Quota exhausted",
		Extras: map[string]any{"quota": "files"},
	}
	rec := httptest.NewRecorder()

	if err := ew.Write(rec, httptest.NewRequest(http.MethodPost, "/files", nil), fmt.Errorf("upload: %w", returned)); err != nil {
		t.Fatal(err)
	}

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want the Problem's 409", rec.Code)
	}
	var got web.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != returned.Type || got.Title != returned.Title || got.Detail != "" || got.Extras["quota"] != "files" || got.Instance != "/files" {
		t.Errorf("problem = %+v, want the returned one with the request path as instance", got)
	}
	if p := ew.Problem(web.Problem{}); p.Status != http.StatusInternalServerError {
		t.Errorf("a returned Problem with no status maps to %d, want 500", p.Status)
	}
}

func TestErrorWriter_PathErrorIsBuiltIn(t *testing.T) {
	ew := web.NewErrorWriter(discard)
	err := &web.PathError{Name: "id", Value: "42"}
	rec := httptest.NewRecorder()

	if werr := ew.Write(rec, httptest.NewRequest(http.MethodGet, "/things/42", nil), err); werr != nil {
		t.Fatal(werr)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `must be a UUID`) {
		t.Errorf("body = %s, want the error text as detail", rec.Body)
	}
}

func TestErrorWriter_BuiltInWinsOverMatchers(t *testing.T) {
	claimAll := func(error) (web.Problem, bool) { return web.Problem{Status: http.StatusTeapot}, true }
	ew := web.NewErrorWriter(discard, claimAll)

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
		if got := ew.Problem(tt.err).Status; got != tt.want {
			t.Errorf("Status(%s) = %d, want %d", name, got, tt.want)
		}
	}
}

func TestErrorWriter_Write428CarriesTheErrorText(t *testing.T) {
	ew := web.NewErrorWriter(discard)
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
		discard,
		matcherFor(sentinel, http.StatusPreconditionFailed),
		matcherFor(sentinel, http.StatusConflict),
	)

	err := fmt.Errorf("update: %w", sentinel)
	if got := ew.Problem(err).Status; got != http.StatusPreconditionFailed {
		t.Errorf("Status = %d, want %d", got, http.StatusPreconditionFailed)
	}
}

func TestErrorWriter_UnclaimedErrorIs500(t *testing.T) {
	ew := web.NewErrorWriter(discard, matcherFor(errors.New("unrelated"), http.StatusConflict))

	if got := ew.Problem(errors.New("driver: connection reset")).Status; got != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", got, http.StatusInternalServerError)
	}
}

func TestErrorWriter_Write400CarriesTheErrorText(t *testing.T) {
	ew := web.NewErrorWriter(discard)
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
		discard,
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

// A matcher's own type, title, and extension members reach the wire
// untouched: the vocabulary this step adds.
func TestErrorWriter_Write_MatcherSuppliedProblemReachesTheWire(t *testing.T) {
	const problemType = "https://example.test/probs/out-of-credit"
	sentinel := errors.New("insufficient balance")
	ew := web.NewErrorWriter(discard, func(err error) (web.Problem, bool) {
		if errors.Is(err, sentinel) {
			return web.Problem{
				Type:   problemType,
				Title:  "You do not have enough credit",
				Status: http.StatusForbidden,
				Extras: map[string]any{"balance": 0},
			}, true
		}
		return web.Problem{}, false
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/charges", nil)
	if err := ew.Write(rec, req, sentinel); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body["type"] != problemType {
		t.Errorf("type = %v, want %q", body["type"], problemType)
	}
	if body["title"] != "You do not have enough credit" {
		t.Errorf("title = %v, want the matcher's title", body["title"])
	}
	if body["balance"] != float64(0) {
		t.Errorf("balance = %v, want 0", body["balance"])
	}
}

// A matcher's own Detail always ships, regardless of the writer's detail
// set: a consumer that writes a detail by hand into its matcher did so on
// purpose, and Detail's set only governs the fallback to the error's text.
func TestErrorWriter_Write_MatcherDetailShipsOutsideTheDetailSet(t *testing.T) {
	sentinel := errors.New("dirty at version 7")
	ew := web.NewErrorWriter(discard, func(err error) (web.Problem, bool) {
		if errors.Is(err, sentinel) {
			return web.Problem{Status: http.StatusConflict, Detail: "schema is dirty at version 7"}, true
		}
		return web.Problem{}, false
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/schema/up", nil)
	if err := ew.Write(rec, req, sentinel); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body["detail"] != "schema is dirty at version 7" {
		t.Errorf("detail = %v, want the matcher's own detail", body["detail"])
	}
}

// A matcher that claims an error without naming a status resolves to 500,
// the same as an error no matcher claims at all.
func TestErrorWriter_Problem_MatcherWithZeroStatusIs500(t *testing.T) {
	sentinel := errors.New("unnamed")
	ew := web.NewErrorWriter(discard, func(err error) (web.Problem, bool) {
		if errors.Is(err, sentinel) {
			return web.Problem{Detail: "no status set"}, true
		}
		return web.Problem{}, false
	})

	if got := ew.Problem(sentinel).Status; got != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", got, http.StatusInternalServerError)
	}
}

func TestErrorWriter_WriteAboveA400SendsNoErrorText(t *testing.T) {
	sentinel := errors.New("unique constraint violation")
	ew := web.NewErrorWriter(discard, matcherFor(sentinel, http.StatusConflict))

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

// A 5xx carries no detail on the wire, so its cause goes to the log with
// the request, its status, and its correlation id: a 503 at warn, any
// other 5xx at error, a client's hang-up at debug. A 4xx is not logged.
func TestErrorWriter_Write5xxLogsTheCause(t *testing.T) {
	errDown := errors.New("store: connection refused")
	cases := map[string]struct {
		err    error
		cancel bool
		status float64
		level  string
	}{
		"unclaimed":      {errors.New("driver: connection reset"), false, 500, "ERROR"},
		"claimed 500":    {errBroken, false, 500, "ERROR"},
		"no status":      {errNoStatus, false, 500, "ERROR"},
		"outage":         {errDown, false, 503, "WARN"},
		"client hang-up": {fmt.Errorf("read body: %w", context.Canceled), true, 500, "DEBUG"},
		"server cancel":  {fmt.Errorf("query: %w", context.Canceled), false, 500, "ERROR"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			ew := web.NewErrorWriter(
				slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
				matcherFor(errDown, http.StatusServiceUnavailable),
				matcherFor(errBroken, http.StatusInternalServerError),
				matcherFor(errNoStatus, 0),
			)
			req := httptest.NewRequest(http.MethodPost, "/things", nil)
			ctx := web.WithRequestID(req.Context(), "req-1")
			if c.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			rec := httptest.NewRecorder()
			if err := ew.Write(rec, req.WithContext(ctx), c.err); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rec.Body.String(), c.err.Error()) {
				t.Errorf("body = %s, want the cause withheld", rec.Body)
			}
			var entry map[string]any
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("log = %q: %v", buf.String(), err)
			}
			want := map[string]any{
				"level":                     c.level,
				"error":                     c.err.Error(),
				"http.request.method":       "POST",
				"url.path":                  "/things",
				"client.address":            "192.0.2.1:1234",
				"http.response.status_code": c.status,
				"request_id":                "req-1",
			}
			for k, v := range want {
				if entry[k] != v {
					t.Errorf("log %s = %v, want %v", k, entry[k], v)
				}
			}
		})
	}

	var buf bytes.Buffer
	ew := web.NewErrorWriter(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), matcherFor(errConflict, http.StatusConflict))
	if err := ew.Write(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), errConflict); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("a claimed 409 logged %q, want nothing", buf.String())
	}
}

// A 500 through Handle is one error record, the writer's: Handle logs only
// what it could not write.
func TestHandle_500IsLoggedOnce(t *testing.T) {
	var buf bytes.Buffer
	ew := web.NewErrorWriter(slog.New(slog.NewJSONHandler(&buf, nil)))
	h := web.Handle(func(http.ResponseWriter, *http.Request) error { return errors.New("boom") }, ew)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if n := strings.Count(buf.String(), "\n"); n != 1 {
		t.Errorf("records = %d (%q), want one", n, buf.String())
	}
}

var (
	errBroken   = errors.New("broken invariant")
	errNoStatus = errors.New("claimed without a status")
)

var errConflict = errors.New("conflict")
