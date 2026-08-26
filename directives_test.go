package web_test

import (
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

var limits = web.Limits{DefaultSize: 25, MaxSize: 100}

func TestParseDirectives_Defaults(t *testing.T) {
	d, err := web.ParseDirectives(url.Values{}, limits)
	if err != nil {
		t.Fatalf("ParseDirectives: %v", err)
	}

	want := web.Directives{Page: 1, Size: 25}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("directives = %+v, want %+v", d, want)
	}
}

func TestParseDirectives_EmptyValuesReadAsOmitted(t *testing.T) {
	q := url.Values{"page": {""}, "size": {""}, "sort": {""}}

	d, err := web.ParseDirectives(q, limits)
	if err != nil {
		t.Fatalf("ParseDirectives: %v", err)
	}

	want := web.Directives{Page: 1, Size: 25}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("directives = %+v, want %+v", d, want)
	}
}

func TestParseDirectives_PageAndSize(t *testing.T) {
	q := url.Values{"page": {"3"}, "size": {"100"}}

	d, err := web.ParseDirectives(q, limits)
	if err != nil {
		t.Fatalf("ParseDirectives: %v", err)
	}

	if d.Page != 3 || d.Size != 100 {
		t.Errorf("page, size = %d, %d, want 3, 100", d.Page, d.Size)
	}
}

func TestParseDirectives_Sort(t *testing.T) {
	tests := []struct {
		name string
		sort []string
		want []web.Sort
	}{
		{"single", []string{"name"}, []web.Sort{{Field: "name"}}},
		{"descending", []string{"-name"}, []web.Sort{{Field: "name", Descending: true}}},
		{
			"comma separated",
			[]string{"name,-code"},
			[]web.Sort{{Field: "name"}, {Field: "code", Descending: true}},
		},
		{
			"repeated parameter",
			[]string{"name", "-code"},
			[]web.Sort{{Field: "name"}, {Field: "code", Descending: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := web.ParseDirectives(url.Values{"sort": tt.sort}, limits)
			if err != nil {
				t.Fatalf("ParseDirectives: %v", err)
			}
			if !reflect.DeepEqual(d.Sort, tt.want) {
				t.Errorf("sort = %+v, want %+v", d.Sort, tt.want)
			}
		})
	}
}

func TestParseDirectives_Rejections(t *testing.T) {
	tests := []struct {
		name  string
		query url.Values
		param string
	}{
		{"page not a number", url.Values{"page": {"two"}}, "page"},
		{"page zero", url.Values{"page": {"0"}}, "page"},
		{"page negative", url.Values{"page": {"-1"}}, "page"},
		{"size not a number", url.Values{"size": {"many"}}, "size"},
		{"size zero", url.Values{"size": {"0"}}, "size"},
		{"size over the cap", url.Values{"size": {"101"}}, "size"},
		{"sort trailing comma", url.Values{"sort": {"name,"}}, "sort"},
		{"sort bare minus", url.Values{"sort": {"-"}}, "sort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := web.ParseDirectives(tt.query, limits)

			var derr *web.DirectiveError
			if !errors.As(err, &derr) {
				t.Fatalf("error = %v, want *DirectiveError", err)
			}
			if derr.Param != tt.param {
				t.Errorf("param = %q, want %q", derr.Param, tt.param)
			}
			if derr.Value == "" || derr.Reason == "" {
				t.Errorf("error carries value %q, reason %q; want both set", derr.Value, derr.Reason)
			}
		})
	}
}

func TestDirectiveError_NamesTheParameter(t *testing.T) {
	err := &web.DirectiveError{Param: "size", Value: "500", Reason: "must be at most 100"}

	msg := err.Error()
	for _, part := range []string{"size", `"500"`, "must be at most 100"} {
		if !strings.Contains(msg, part) {
			t.Errorf("Error() = %q, missing %q", msg, part)
		}
	}
}

func TestParseDirectives_InvalidLimitsPanic(t *testing.T) {
	tests := []struct {
		name   string
		limits web.Limits
	}{
		{"zero value", web.Limits{}},
		{"default below one", web.Limits{DefaultSize: 0, MaxSize: 100}},
		{"max below default", web.Limits{DefaultSize: 25, MaxSize: 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("ParseDirectives did not panic")
				}
			}()
			_, _ = web.ParseDirectives(url.Values{}, tt.limits)
		})
	}
}
