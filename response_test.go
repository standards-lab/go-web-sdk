package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

func TestWriteJSON_SetsMediaTypeAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()

	err := web.WriteJSON(rec, http.StatusCreated, map[string]string{"id": "42"})
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != web.JSONMediaType {
		t.Errorf("Content-Type = %q, want %q", got, web.JSONMediaType)
	}
	if got := decodeBody(t, rec)["id"]; got != "42" {
		t.Errorf("id = %v, want 42", got)
	}
}

func TestNewPage_CarriesTheQuery(t *testing.T) {
	d := web.Query{Page: 2, Size: 25}

	p := web.NewPage([]string{"a", "b"}, d, web.Paging{Total: 51, More: true, Next: "c2"})

	total := 51
	want := web.Page[string]{Items: []string{"a", "b"}, Page: 2, Size: 25, Total: &total, More: true, Next: "c2"}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("page = %+v, want %+v", p, want)
	}
}

func TestNewPage_NilItemsMarshalAsEmptyArray(t *testing.T) {
	p := web.NewPage[string](nil, web.Query{Page: 1, Size: 25}, web.Paging{})

	body, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"items":[],"page":1,"size":25,"total":0,"more":false}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPage_WireShape(t *testing.T) {
	type row struct {
		Name string `json:"name"`
	}

	body, err := json.Marshal(web.NewPage([]row{{Name: "ops"}}, web.Query{Page: 3, Size: 10}, web.Paging{Total: 21, More: true}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"items":[{"name":"ops"}],"page":3,"size":10,"total":21,"more":true}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPage_CursorPageOmitsTheNumberAndAnUncountedTotal(t *testing.T) {
	q := web.Query{Size: 10, Cursor: "c1"}

	body, err := json.Marshal(web.NewPage([]string{"x"}, q, web.Paging{Total: -1, More: true, Next: "c2"}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"items":["x"],"size":10,"more":true,"next":"c2"}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPage_ZeroTotalIsCountedNotAbsent(t *testing.T) {
	p := web.NewPage[string](nil, web.Query{Page: 1, Size: 10}, web.Paging{Total: 0})

	if p.Total == nil || *p.Total != 0 {
		t.Errorf("total = %v, want a counted 0", p.Total)
	}
}

func objectRequest(method string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, "/files/1/content", nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return r
}

var logo = web.Object{
	ContentType: "image/png",
	Size:        9,
	ETag:        `"0x8DC"`,
	ModifiedAt:  time.Date(2026, 9, 24, 12, 0, 0, 0, time.FixedZone("EDT", -4*3600)),
}

func TestWriteObject_ProxiesTheBytesWithTheirHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Disposition", `inline; filename="logo.png"`)

	if err := web.WriteObject(rec, objectRequest(http.MethodGet), logo, strings.NewReader("png-bytes")); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	want := map[string]string{
		"Content-Type":           "image/png",
		"Content-Length":         "9",
		"ETag":                   `"0x8DC"`,
		"Last-Modified":          "Thu, 24 Sep 2026 16:00:00 GMT",
		"X-Content-Type-Options": "nosniff",
		"Content-Disposition":    `inline; filename="logo.png"`,
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	if rec.Body.String() != "png-bytes" {
		t.Errorf("body = %q, want png-bytes", rec.Body.String())
	}
}

func TestWriteObject_MatchingIfNoneMatchIs304(t *testing.T) {
	for _, header := range []string{`"0x8DC"`, `W/"0x8DC"`, `"other", "0x8DC"`, "*"} {
		t.Run(header, func(t *testing.T) {
			rec := httptest.NewRecorder()

			err := web.WriteObject(rec, objectRequest(http.MethodGet, "If-None-Match", header), logo, strings.NewReader("png-bytes"))
			if err != nil {
				t.Fatalf("WriteObject: %v", err)
			}

			if rec.Code != http.StatusNotModified {
				t.Errorf("status = %d, want 304", rec.Code)
			}
			if rec.Body.Len() != 0 {
				t.Errorf("body = %q, want none", rec.Body.String())
			}
			if got := rec.Header().Get("ETag"); got != `"0x8DC"` {
				t.Errorf("ETag = %q, want the object's", got)
			}
		})
	}
}

func TestWriteObject_StaleIfNoneMatchSendsTheBytes(t *testing.T) {
	rec := httptest.NewRecorder()

	err := web.WriteObject(rec, objectRequest(http.MethodGet, "If-None-Match", `"0x1"`), logo, strings.NewReader("png-bytes"))
	if err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	if rec.Code != http.StatusOK || rec.Body.String() != "png-bytes" {
		t.Errorf("status, body = %d, %q, want 200, png-bytes", rec.Code, rec.Body.String())
	}
}

func TestWriteObject_HeadSendsHeadersOnly(t *testing.T) {
	rec := httptest.NewRecorder()

	if err := web.WriteObject(rec, objectRequest(http.MethodHead), logo, strings.NewReader("png-bytes")); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("status, body = %d, %q, want 200 and none", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Length"); got != "9" {
		t.Errorf("Content-Length = %q, want 9", got)
	}
}

func TestWriteObject_OmitsAbsentValidators(t *testing.T) {
	rec := httptest.NewRecorder()

	err := web.WriteObject(rec, objectRequest(http.MethodGet, "If-None-Match", `"x"`), web.Object{ContentType: "text/plain", Size: 2}, strings.NewReader("hi"))
	if err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	if rec.Header().Get("ETag") != "" || rec.Header().Get("Last-Modified") != "" {
		t.Errorf("validators = %q, %q, want none", rec.Header().Get("ETag"), rec.Header().Get("Last-Modified"))
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
