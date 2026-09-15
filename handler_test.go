package web_test

import (
	"bytes"
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

func TestHandle_WritesAReturnedErrorAsAProblem(t *testing.T) {
	sentinel := errors.New("row is gone")
	ew := web.NewErrorWriter(matcherFor(sentinel, http.StatusNotFound))
	h := web.Handle(func(http.ResponseWriter, *http.Request) error {
		return fmt.Errorf("find: %w", sentinel)
	}, ew)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things/1", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
		t.Errorf("content type = %q, want %q", ct, web.ProblemMediaType)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body["instance"] != "/things/1" {
		t.Errorf("instance = %q, want %q", body["instance"], "/things/1")
	}
}

func TestHandle_LeavesASuccessfulResponseAlone(t *testing.T) {
	h := web.Handle(func(w http.ResponseWriter, _ *http.Request) error {
		return web.WriteJSON(w, http.StatusCreated, map[string]string{"id": "7"})
	}, web.NewErrorWriter())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/things", nil))

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != web.JSONMediaType {
		t.Errorf("content type = %q, want %q", ct, web.JSONMediaType)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"id":"7"}` {
		t.Errorf("body = %q, want %q", got, `{"id":"7"}`)
	}
}

func TestHandle_NeverWritesASecondResponse(t *testing.T) {
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
			var log bytes.Buffer
			ew := web.NewErrorWriter()
			ew.Log(slog.New(slog.NewJSONHandler(&log, nil)))
			h := web.Handle(func(w http.ResponseWriter, _ *http.Request) error {
				tt.commit(w)
				return errors.New("stream broke")
			}, ew)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things", nil))

			if rec.Code != tt.status {
				t.Errorf("status = %d, want the committed %d", rec.Code, tt.status)
			}
			if strings.Contains(rec.Body.String(), "problem") || rec.Header().Get("Content-Type") == web.ProblemMediaType {
				t.Errorf("a problem was written after the commit: %q", rec.Body.String())
			}

			var record map[string]any
			if err := json.Unmarshal(log.Bytes(), &record); err != nil {
				t.Fatalf("no log record: %v (%q)", err, log.String())
			}
			if record["level"] != "ERROR" {
				t.Errorf("level = %v, want ERROR", record["level"])
			}
			if record["error"] != "stream broke" || record["url.path"] != "/things" || record["http.request.method"] != http.MethodGet {
				t.Errorf("record = %v, want the error, path, and method", record)
			}
			if int(record["http.response.status_code"].(float64)) != tt.status {
				t.Errorf("status attr = %v, want %d", record["http.response.status_code"], tt.status)
			}
		})
	}
}

// brokenWriter is a ResponseWriter whose Write fails as a dropped client
// connection does: headers and status go through, the body does not.
type brokenWriter struct {
	http.ResponseWriter
	err error
}

func (w brokenWriter) Write([]byte) (int, error) { return 0, w.err }

func TestHandle_LogsAProblemWriteFailure(t *testing.T) {
	sentinel := errors.New("row is gone")
	broken := errors.New("write tcp: connection reset by peer")
	var log bytes.Buffer
	ew := web.NewErrorWriter(matcherFor(sentinel, http.StatusNotFound))
	ew.Log(slog.New(slog.NewJSONHandler(&log, nil)))
	h := web.Handle(func(http.ResponseWriter, *http.Request) error {
		return sentinel
	}, ew)

	rec := httptest.NewRecorder()
	h.ServeHTTP(brokenWriter{rec, broken}, httptest.NewRequest(http.MethodGet, "/things/1", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (the header goes through; only the body fails)", rec.Code, http.StatusNotFound)
	}
	var record map[string]any
	if err := json.Unmarshal(log.Bytes(), &record); err != nil {
		t.Fatalf("no log record: %v (%q)", err, log.String())
	}
	for _, tc := range []struct {
		key  string
		want any
	}{
		{"msg", "failed to write problem response"},
		{"level", "ERROR"},
		{"http.request.method", http.MethodGet},
		{"url.path", "/things/1"},
		{"http.response.status_code", float64(http.StatusNotFound)},
		{"error", broken.Error()},
	} {
		if got := record[tc.key]; got != tc.want {
			t.Errorf("%s = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestHandle_NilWriterPanics(t *testing.T) {
	mustPanic(t, "Handle(fn, nil)", func() {
		web.Handle(func(http.ResponseWriter, *http.Request) error { return nil }, nil)
	})
}

// The end-to-end check for the step: a route group of error-returning
// handlers using the request helpers, compiled into a module and served
// through a router, one request per outcome the SDK maps.
func TestHandleErr_EndToEnd(t *testing.T) {
	errConflict := errors.New("name already taken")
	errInternal := errors.New("driver: connection reset")

	type edit struct {
		Name string `json:"name"`
	}

	ew := web.NewErrorWriter(matcherFor(errConflict, http.StatusConflict))
	ew.Detail(http.StatusConflict)
	ew.Log(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))

	things := web.NewGroup("/things")
	things.SetErrorWriter(ew)
	things.HandleErr(http.MethodGet, "", func(w http.ResponseWriter, r *http.Request) error {
		q, err := web.ParseQuery(r.URL.Query(), web.Limits{DefaultSize: 10, MaxSize: 50})
		if err != nil {
			return err
		}
		return web.WriteJSON(w, http.StatusOK, web.NewPage([]string{"a"}, q, 1))
	})
	things.HandleErr(http.MethodPatch, "/{id}", func(w http.ResponseWriter, r *http.Request) error {
		version, err := web.IfMatch(r)
		if err != nil {
			return err
		}
		body, err := web.DecodeJSON[edit](w, r, 64)
		if err != nil {
			return err
		}
		switch body.Name {
		case "taken":
			return fmt.Errorf("edit: %w", errConflict)
		case "boom":
			return errInternal
		}
		return web.WriteJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "version": version + 1, "name": body.Name})
	})

	api := web.NewGroup("/api")
	api.Mount(things)
	router := web.NewRouter()
	router.Mount(web.NewModule(api))

	patch := func(ifMatch, body string) *http.Request {
		req := httptest.NewRequest(http.MethodPatch, "/api/things/42", strings.NewReader(body))
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		return req
	}

	tests := []struct {
		name   string
		req    *http.Request
		status int
		detail string // "" means the detail member must be absent
	}{
		{"list", httptest.NewRequest(http.MethodGet, "/api/things?size=5&name[like]=a%25", nil), http.StatusOK, ""},
		{"list with a bad page", httptest.NewRequest(http.MethodGet, "/api/things?page=0", nil), http.StatusBadRequest, `query page="0"`},
		{"list with an operator on a reserved name", httptest.NewRequest(http.MethodGet, "/api/things?size[gt]=1", nil), http.StatusBadRequest, "takes no operator"},
		{"edit", patch(`"3"`, `{"name": "widget"}`), http.StatusOK, ""},
		{"edit without If-Match", patch("", `{"name": "widget"}`), http.StatusPreconditionRequired, "requires an If-Match"},
		{"edit with a weak tag", patch(`W/"3"`, `{"name": "widget"}`), http.StatusBadRequest, "If-Match"},
		{"edit with an unknown field", patch(`"3"`, `{"nmae": "widget"}`), http.StatusBadRequest, `unknown field "nmae"`},
		{"edit with an oversized body", patch(`"3"`, `{"name": "`+strings.Repeat("x", 100)+`"}`), http.StatusRequestEntityTooLarge, "64-byte limit"},
		{"edit into a conflict", patch(`"3"`, `{"name": "taken"}`), http.StatusConflict, "name already taken"},
		{"edit that fails internally", patch(`"3"`, `{"name": "boom"}`), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, tt.req)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.status, rec.Body.String())
			}
			if tt.status < 400 {
				return
			}
			var problem map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			detail, present := problem["detail"]
			if tt.detail == "" && present {
				t.Errorf("detail = %q, want it absent", detail)
			}
			if tt.detail != "" && !strings.Contains(fmt.Sprint(detail), tt.detail) {
				t.Errorf("detail = %q, want it to contain %q", detail, tt.detail)
			}
			if problem["instance"] != tt.req.URL.Path {
				t.Errorf("instance = %q, want %q", problem["instance"], tt.req.URL.Path)
			}
		})
	}
}

// Both of Handle's failure records — an error after a committed response and
// a failed problem write — carry request_id when the request's context holds
// one, and omit it otherwise.
func TestHandle_RequestIDOnFailureRecords(t *testing.T) {
	withID := func(path, id string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		return req.WithContext(web.WithRequestID(req.Context(), id))
	}
	sentinel := errors.New("row is gone")
	sites := []struct {
		name string
		fn   web.HandlerFunc
		w    func(*httptest.ResponseRecorder) http.ResponseWriter
		msg  string
	}{
		{
			"error after commit",
			func(w http.ResponseWriter, _ *http.Request) error {
				w.WriteHeader(http.StatusAccepted)
				return errors.New("stream broke")
			},
			func(rec *httptest.ResponseRecorder) http.ResponseWriter { return rec },
			"handler returned an error after committing its response",
		},
		{
			"problem write failure",
			func(http.ResponseWriter, *http.Request) error { return sentinel },
			func(rec *httptest.ResponseRecorder) http.ResponseWriter {
				return brokenWriter{rec, errors.New("reset")}
			},
			"failed to write problem response",
		},
	}
	requests := []struct {
		name string
		req  *http.Request
		id   any // nil means the attribute must be absent
	}{
		{"with an id", withID("/things/1", "abc123"), "abc123"},
		{"without an id", httptest.NewRequest(http.MethodGet, "/things/1", nil), nil},
	}
	for _, site := range sites {
		for _, tt := range requests {
			t.Run(site.name+" "+tt.name, func(t *testing.T) {
				var log bytes.Buffer
				ew := web.NewErrorWriter(matcherFor(sentinel, http.StatusNotFound))
				ew.Log(slog.New(slog.NewJSONHandler(&log, nil)))

				web.Handle(site.fn, ew).ServeHTTP(site.w(httptest.NewRecorder()), tt.req)

				var record map[string]any
				if err := json.Unmarshal(log.Bytes(), &record); err != nil {
					t.Fatalf("no log record: %v (%q)", err, log.String())
				}
				if record["msg"] != site.msg {
					t.Fatalf("msg = %v, want %q", record["msg"], site.msg)
				}
				got, present := record["request_id"]
				if tt.id == nil && present {
					t.Errorf("request_id = %v, want it absent", got)
				}
				if tt.id != nil && got != tt.id {
					t.Errorf("request_id = %v, want %v", got, tt.id)
				}
			})
		}
	}
}
