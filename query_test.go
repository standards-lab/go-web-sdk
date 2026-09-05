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

func TestParseQuery_Defaults(t *testing.T) {
	q, err := web.ParseQuery(url.Values{}, limits)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	want := web.Query{Page: 1, Size: 25, Filters: []web.Filter{}}
	if !reflect.DeepEqual(q, want) {
		t.Errorf("query = %+v, want %+v", q, want)
	}
}

func TestParseQuery_EmptyValuesReadAsOmitted(t *testing.T) {
	q, err := web.ParseQuery(url.Values{"page": {""}, "size": {""}, "sort": {""}}, limits)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	want := web.Query{Page: 1, Size: 25, Filters: []web.Filter{}}
	if !reflect.DeepEqual(q, want) {
		t.Errorf("query = %+v, want %+v", q, want)
	}
}

func TestParseQuery_PageAndSize(t *testing.T) {
	q, err := web.ParseQuery(url.Values{"page": {"3"}, "size": {"100"}}, limits)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	if q.Page != 3 || q.Size != 100 {
		t.Errorf("page, size = %d, %d, want 3, 100", q.Page, q.Size)
	}
}

func TestParseQuery_Sort(t *testing.T) {
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
			q, err := web.ParseQuery(url.Values{"sort": tt.sort}, limits)
			if err != nil {
				t.Fatalf("ParseQuery: %v", err)
			}
			if !reflect.DeepEqual(q.Sort, tt.want) {
				t.Errorf("sort = %+v, want %+v", q.Sort, tt.want)
			}
		})
	}
}

func TestParseQuery_SplitsFiltersFromDirectiveParameters(t *testing.T) {
	q, err := web.ParseQuery(url.Values{
		"page":   {"2"},
		"size":   {"50"},
		"sort":   {"name"},
		"status": {"active"},
		"unit":   {"ops", "lab"},
	}, limits)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	want := []web.Filter{
		{Field: "status", Values: []string{"active"}},
		{Field: "unit", Values: []string{"ops", "lab"}},
	}
	if !reflect.DeepEqual(q.Filters, want) {
		t.Errorf("filters = %+v, want %+v", q.Filters, want)
	}
}

func TestParseQuery_FiltersAreNeverNil(t *testing.T) {
	q, err := web.ParseQuery(url.Values{"page": {"2"}}, limits)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	if q.Filters == nil {
		t.Error("filters = nil, want an empty slice")
	}
}

func TestParseQuery_FilterOperators(t *testing.T) {
	q, err := web.ParseQuery(url.Values{
		"created[gte]": {"2026-01-01T10:00:00Z"},
		"created[lt]":  {"2026-02-01T00:00:00Z"},
		"name[like]":   {"acme%"},
		"status[in]":   {"active", "pending"},
		"unit":         {"ops"},
	}, limits)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}

	want := []web.Filter{
		{Field: "created", Op: "gte", Values: []string{"2026-01-01T10:00:00Z"}},
		{Field: "created", Op: "lt", Values: []string{"2026-02-01T00:00:00Z"}},
		{Field: "name", Op: "like", Values: []string{"acme%"}},
		{Field: "status", Op: "in", Values: []string{"active", "pending"}},
		{Field: "unit", Values: []string{"ops"}},
	}
	if !reflect.DeepEqual(q.Filters, want) {
		t.Errorf("filters = %+v, want %+v", q.Filters, want)
	}
}

func TestParseQuery_OperatorsPassThroughAsText(t *testing.T) {
	q, err := web.ParseQuery(url.Values{"name[frobnicate]": {"x"}}, limits)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if len(q.Filters) != 1 || q.Filters[0].Op != "frobnicate" {
		t.Errorf("filters = %+v, want the operator carried verbatim", q.Filters)
	}
}

func TestParseQuery_FiltersAreOrdered(t *testing.T) {
	values := url.Values{
		"zeta":      {"1"},
		"alpha[lt]": {"2"},
		"alpha[gt]": {"3"},
		"mid":       {"4"},
	}
	want := []string{"alpha gt", "alpha lt", "mid ", "zeta "}
	for range 20 {
		q, err := web.ParseQuery(values, limits)
		if err != nil {
			t.Fatalf("ParseQuery: %v", err)
		}
		var got []string
		for _, f := range q.Filters {
			got = append(got, f.Field+" "+f.Op)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("filter order = %v, want %v", got, want)
		}
	}
}

func TestParseQuery_RejectsMalformedFilterKeys(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		param string
	}{
		{"unclosed", "name[gt", "name[gt"},
		{"unopened", "name]", "name]"},
		{"stray close", "name]gt[", "name]gt["},
		{"no field", "[gt]", "[gt]"},
		{"empty operator", "name[]", "name[]"},
		{"two operators", "name[gt][lt]", "name[gt][lt]"},
		{"text after the bracket", "name[gt]x", "name[gt]x"},
		{"reserved page", "page[gt]", "page"},
		{"reserved size", "size[lt]", "size"},
		{"reserved sort", "sort[eq]", "sort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := web.ParseQuery(url.Values{tt.key: {"5"}}, limits)

			qerr, ok := errors.AsType[*web.QueryError](err)
			if !ok {
				t.Fatalf("error = %v, want *QueryError", err)
			}
			if qerr.Param != tt.param {
				t.Errorf("param = %q, want %q", qerr.Param, tt.param)
			}
			if qerr.Value != "5" || qerr.Reason == "" {
				t.Errorf("error carries value %q, reason %q; want the first value and a reason", qerr.Value, qerr.Reason)
			}
		})
	}
}

func TestParseQuery_Rejections(t *testing.T) {
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
			_, err := web.ParseQuery(tt.query, limits)

			qerr, ok := errors.AsType[*web.QueryError](err)
			if !ok {
				t.Fatalf("error = %v, want *QueryError", err)
			}
			if qerr.Param != tt.param {
				t.Errorf("param = %q, want %q", qerr.Param, tt.param)
			}
			if qerr.Value == "" || qerr.Reason == "" {
				t.Errorf("error carries value %q, reason %q; want both set", qerr.Value, qerr.Reason)
			}
		})
	}
}

func TestQueryError_NamesTheParameter(t *testing.T) {
	err := &web.QueryError{Param: "size", Value: "500", Reason: "must be at most 100"}

	msg := err.Error()
	for _, part := range []string{"size", `"500"`, "must be at most 100"} {
		if !strings.Contains(msg, part) {
			t.Errorf("Error() = %q, missing %q", msg, part)
		}
	}
}

func TestParseQuery_InvalidLimitsPanic(t *testing.T) {
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
					t.Error("ParseQuery did not panic")
				}
			}()
			_, _ = web.ParseQuery(url.Values{}, tt.limits)
		})
	}
}
