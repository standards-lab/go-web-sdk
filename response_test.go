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

	p := web.NewPage([]string{"a", "b"}, d, 51)

	want := web.Page[string]{Items: []string{"a", "b"}, Page: 2, Size: 25, Total: 51}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("page = %+v, want %+v", p, want)
	}
}

func TestNewPage_NilItemsMarshalAsEmptyArray(t *testing.T) {
	p := web.NewPage[string](nil, web.Query{Page: 1, Size: 25}, 0)

	body, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"items":[],"page":1,"size":25,"total":0}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestPage_WireShape(t *testing.T) {
	type row struct {
		Name string `json:"name"`
	}

	body, err := json.Marshal(web.NewPage([]row{{Name: "ops"}}, web.Query{Page: 3, Size: 10}, 21))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"items":[{"name":"ops"}],"page":3,"size":10,"total":21}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}
