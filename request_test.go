package web_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

	if u.ContentType != "image/png" || u.MediaType != "image/png" || u.Size != 9 {
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
	u, _, err := upload(t, strings.NewReader("a"), "Text/Plain; charset=utf-8", 1, 64)
	if err != nil {
		t.Fatalf("ReadUpload: %v", err)
	}

	if u.ContentType != "Text/Plain; charset=utf-8" {
		t.Errorf("ContentType = %q, want the declared value whole", u.ContentType)
	}
	if u.MediaType != "text/plain" {
		t.Errorf("MediaType = %q, want text/plain", u.MediaType)
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
	// The body overruns its declared length: the bound holds even where
	// net/http's own length enforcement does not apply.
	u, _, err := upload(t, strings.NewReader(strings.Repeat("x", 100)), "text/plain", 8, 8)
	if err != nil {
		t.Fatalf("ReadUpload: %v", err)
	}

	_, err = io.ReadAll(u.Body)
	if _, ok := errors.AsType[*http.MaxBytesError](err); !ok {
		t.Errorf("read error = %v, want *http.MaxBytesError", err)
	}
}

// On a live server, a request with neither a length nor chunked encoding
// has no body and arrives as a 0-byte upload; a chunked one is refused
// with a 411.
func TestReadUpload_OnTheWire(t *testing.T) {
	srv := httptest.NewServer(web.Handle(func(w http.ResponseWriter, r *http.Request) error {
		u, err := web.ReadUpload(w, r, 64)
		if err != nil {
			return err
		}
		n, err := io.Copy(io.Discard, u.Body)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "size=%d read=%d", u.Size, n)
		return err
	}, web.NewErrorWriter()))
	defer srv.Close()

	tests := []struct {
		name    string
		request string
		status  int
		body    string
	}{
		{
			"no length header",
			"PUT / HTTP/1.1\r\nHost: x\r\nContent-Type: image/png\r\nConnection: close\r\n\r\n",
			http.StatusOK, "size=0 read=0",
		},
		{
			"chunked",
			"PUT / HTTP/1.1\r\nHost: x\r\nContent-Type: image/png\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n3\r\nabc\r\n0\r\n\r\n",
			http.StatusLengthRequired, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", srv.Listener.Addr().String())
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer func() { _ = conn.Close() }()
			if _, err := io.WriteString(conn, tt.request); err != nil {
				t.Fatalf("write: %v", err)
			}
			res, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()

			if res.StatusCode != tt.status {
				t.Errorf("status = %d, want %d; body: %s", res.StatusCode, tt.status, body)
			}
			if tt.body != "" && string(body) != tt.body {
				t.Errorf("body = %q, want %q", body, tt.body)
			}
		})
	}
}

func TestUploadError_ConsumerRefusalIs415(t *testing.T) {
	err := &web.UploadError{Header: "Content-Type", Reason: "image/gif is not an accepted logo type"}

	if got := web.NewErrorWriter().Status(err); got != http.StatusUnsupportedMediaType {
		t.Errorf("Status = %d, want 415", got)
	}
}
