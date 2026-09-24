package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

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
