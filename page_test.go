package web_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

func TestNewPage_CarriesTheDirectives(t *testing.T) {
	d := web.Directives{Page: 2, Size: 25}

	p := web.NewPage([]string{"a", "b"}, d, 51)

	want := web.Page[string]{Items: []string{"a", "b"}, Page: 2, Size: 25, Total: 51}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("page = %+v, want %+v", p, want)
	}
}

func TestNewPage_NilItemsMarshalAsEmptyArray(t *testing.T) {
	p := web.NewPage[string](nil, web.Directives{Page: 1, Size: 25}, 0)

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

	body, err := json.Marshal(web.NewPage([]row{{Name: "ops"}}, web.Directives{Page: 3, Size: 10}, 21))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"items":[{"name":"ops"}],"page":3,"size":10,"total":21}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}
