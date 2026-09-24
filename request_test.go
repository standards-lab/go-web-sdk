package web_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

func ifMatch(t *testing.T, header string) (int64, error) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPatch, "/", nil)
	if header != "" {
		r.Header.Set("If-Match", header)
	}
	return web.IfMatch(r)
}

func TestIfMatch_ParsesStrongIntegerTags(t *testing.T) {
	cases := map[string]int64{
		`"3"`:   3,
		`"0"`:   0,
		` "7" `: 7,
		`"-1"`:  -1,
	}
	for header, want := range cases {
		got, err := ifMatch(t, header)
		if err != nil {
			t.Errorf("IfMatch(%q) error: %v", header, err)
			continue
		}
		if got != want {
			t.Errorf("IfMatch(%q) = %d, want %d", header, got, want)
		}
	}
}

func TestIfMatch_MissingHeader(t *testing.T) {
	_, err := ifMatch(t, "")

	pre, ok := errors.AsType[*web.PreconditionError](err)
	if !ok {
		t.Fatalf("error = %v, want *PreconditionError", err)
	}
	if !pre.Missing {
		t.Error("Missing = false, want true for an absent header")
	}
	if pre.Error() == "" {
		t.Error("missing-header error carries no message")
	}
}

func TestIfMatch_RejectsMalformedTags(t *testing.T) {
	cases := []string{`3`, `*`, `W/"3"`, `""`, `"abc"`, `"1", "2"`}
	for _, header := range cases {
		_, err := ifMatch(t, header)

		pre, ok := errors.AsType[*web.PreconditionError](err)
		if !ok {
			t.Errorf("IfMatch(%q) error = %v, want *PreconditionError", header, err)
			continue
		}
		if pre.Missing {
			t.Errorf("IfMatch(%q): Missing = true, want false for a present header", header)
		}
		if pre.Value != header {
			t.Errorf("IfMatch(%q): Value = %q, want the header text", header, pre.Value)
		}
	}
}

type command struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func decode(t *testing.T, body string, limit int64) (command, *httptest.ResponseRecorder, error) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/things", strings.NewReader(body))
	v, err := web.DecodeJSON[command](rec, req, limit)
	return v, rec, err
}

func TestDecodeJSON_ReadsOneValue(t *testing.T) {
	got, _, err := decode(t, `{"name": "widget", "count": 3}`, 1<<10)
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	want := command{Name: "widget", Count: 3}
	if got != want {
		t.Errorf("value = %+v, want %+v", got, want)
	}
}

func TestDecodeJSON_RejectsWithReason(t *testing.T) {
	cases := map[string]string{
		"unknown field":  `{"name": "widget", "colour": "red"}`,
		"malformed":      `{"name": "widget",`,
		"wrong type":     `{"name": "widget", "count": "three"}`,
		"trailing value": `{"name": "widget"} {"name": "gadget"}`,
		"trailing text":  `{"name": "widget"} extra`,
		"empty":          ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := decode(t, body, 1<<10)

			berr, ok := errors.AsType[*web.BodyError](err)
			if !ok {
				t.Fatalf("error = %v, want *BodyError", err)
			}
			if berr.TooLarge {
				t.Error("TooLarge = true, want false")
			}
			if berr.Reason == "" {
				t.Error("Reason is empty")
			}
			if !strings.HasPrefix(berr.Error(), "body: ") {
				t.Errorf("Error() = %q, want the body: prefix", berr.Error())
			}
		})
	}
}

func TestDecodeJSON_EmptyBodyNamesItself(t *testing.T) {
	_, _, err := decode(t, ``, 1<<10)

	berr, ok := errors.AsType[*web.BodyError](err)
	if !ok {
		t.Fatalf("error = %v, want *BodyError", err)
	}
	if berr.Reason != "empty body" {
		t.Errorf("Reason = %q, want %q", berr.Reason, "empty body")
	}
}

func TestDecodeJSON_OverflowIsTooLarge(t *testing.T) {
	body := `{"name": "` + strings.Repeat("x", 64) + `"}`
	_, _, err := decode(t, body, 16)

	berr, ok := errors.AsType[*web.BodyError](err)
	if !ok {
		t.Fatalf("error = %v, want *BodyError", err)
	}
	if !berr.TooLarge {
		t.Error("TooLarge = false, want true")
	}
	if !strings.Contains(berr.Reason, "16") {
		t.Errorf("Reason = %q, want it to name the limit", berr.Reason)
	}
}

func TestDecodeJSON_OverflowAnswers413WithTheReason(t *testing.T) {
	body := `{"name": "` + strings.Repeat("x", 64) + `"}`
	_, rec, err := decode(t, body, 16)
	if err == nil {
		t.Fatal("DecodeJSON: want an error")
	}

	ew := web.NewErrorWriter()
	req := httptest.NewRequest(http.MethodPost, "/things", nil)
	if werr := ew.Write(rec, req, err); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	var problem map[string]any
	if uerr := json.Unmarshal(rec.Body.Bytes(), &problem); uerr != nil {
		t.Fatalf("Unmarshal: %v", uerr)
	}
	if problem["detail"] != err.Error() {
		t.Errorf("detail = %q, want %q", problem["detail"], err.Error())
	}
}

func TestDecodeJSON_RejectionAnswers400(t *testing.T) {
	_, rec, err := decode(t, `{"colour": "red"}`, 1<<10)
	if err == nil {
		t.Fatal("DecodeJSON: want an error")
	}

	ew := web.NewErrorWriter()
	req := httptest.NewRequest(http.MethodPost, "/things", nil)
	if werr := ew.Write(rec, req, err); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func upload(t *testing.T, body io.Reader, contentType string, length, limit int64) (web.Upload, *httptest.ResponseRecorder, error) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "/files/logo.png", body)
	r.ContentLength = length
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	u, err := web.ReadUpload(rec, r, limit)
	return u, rec, err
}

func TestReadUpload_AcceptsADeclaredBody(t *testing.T) {
	u, _, err := upload(t, strings.NewReader("png-bytes"), "image/png", 9, 64)
	if err != nil {
		t.Fatalf("ReadUpload: %v", err)
	}

	if u.ContentType != "image/png" || u.Size != 9 {
		t.Errorf("upload = %q, %d, want image/png, 9", u.ContentType, u.Size)
	}
	got, err := io.ReadAll(u.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != "png-bytes" {
		t.Errorf("body = %q, want png-bytes", got)
	}
}

func TestReadUpload_KeepsMediaTypeParameters(t *testing.T) {
	u, _, err := upload(t, strings.NewReader("a"), "text/plain; charset=utf-8", 1, 64)
	if err != nil {
		t.Fatalf("ReadUpload: %v", err)
	}

	if u.ContentType != "text/plain; charset=utf-8" {
		t.Errorf("ContentType = %q, want the declared value whole", u.ContentType)
	}
}

func TestReadUpload_EmptyBodyIsAnUpload(t *testing.T) {
	u, _, err := upload(t, http.NoBody, "text/plain", 0, 64)
	if err != nil {
		t.Fatalf("ReadUpload: %v", err)
	}

	if u.Size != 0 {
		t.Errorf("Size = %d, want 0", u.Size)
	}
}

func TestReadUpload_Refusals(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		length      int64
		status      int
	}{
		{"no content type", "", 3, http.StatusUnsupportedMediaType},
		{"unparsable content type", "image/", 3, http.StatusUnsupportedMediaType},
		{"no content length", "image/png", -1, http.StatusLengthRequired},
		{"declared over the limit", "image/png", 65, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, rec, err := upload(t, strings.NewReader("abc"), tt.contentType, tt.length, 64)

			if _, ok := errors.AsType[*web.UploadError](err); !ok {
				t.Fatalf("error = %v, want *UploadError", err)
			}
			ew := web.NewErrorWriter()
			if werr := ew.Write(rec, httptest.NewRequest(http.MethodPut, "/files/logo.png", nil), err); werr != nil {
				t.Fatalf("Write: %v", werr)
			}
			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
			var problem map[string]any
			if uerr := json.Unmarshal(rec.Body.Bytes(), &problem); uerr != nil {
				t.Fatalf("Unmarshal: %v", uerr)
			}
			if problem["detail"] != err.Error() {
				t.Errorf("detail = %q, want %q", problem["detail"], err.Error())
			}
		})
	}
}

func TestReadUpload_BodyIsBoundedAtTheLimit(t *testing.T) {
	// A length the handler was told but the body overruns: the bound holds
	// even where net/http's own length enforcement does not apply.
	u, _, err := upload(t, strings.NewReader(strings.Repeat("x", 100)), "text/plain", 8, 8)
	if err != nil {
		t.Fatalf("ReadUpload: %v", err)
	}

	_, err = io.ReadAll(u.Body)
	if _, ok := errors.AsType[*http.MaxBytesError](err); !ok {
		t.Errorf("read error = %v, want *http.MaxBytesError", err)
	}
}
