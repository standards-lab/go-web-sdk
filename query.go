package web

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// reserved names the parameters [ParseQuery] consumes; every other one is a
// filter.
var reserved = map[string]struct{}{
	"page":   {},
	"size":   {},
	"sort":   {},
	"cursor": {},
}

// Sort is one sort key: a field name, and whether the request prefixed it
// with "-" for descending.
type Sort struct {
	Field      string
	Descending bool
}

// Filter is one filter parameter: a field name, the operator the request
// named in brackets after it ("created[gte]"), empty for the plain form
// ("status"), and every value the parameter carried. The consumer decides
// what an operator and several values mean; the SDK enumerates no operators.
// A key with no name, unbalanced or repeated brackets, or an empty operator
// is rejected, as is an operator on a reserved parameter.
type Filter struct {
	Field  string
	Op     string
	Values []string
}

// Query is one read request's parsed query string. A read addressed by
// number has a 1-based Page, 1 when absent and bounded so the offset fits an
// int. A read addressed by Cursor, the token a previous page's [Page.Next]
// carried, has Page 0 and continues after that page's last item, so it
// neither skips nor repeats a row when the collection changes. A request
// naming both is rejected. [Limits] defaults and caps Size. Sort holds the
// keys of every sort parameter in request order, nil when there are none.
// Filters is never nil and is ordered by field, then operator, so a consumer
// composes a deterministic predicate. An empty parameter value reads as
// omitted.
type Query struct {
	Page    int
	Size    int
	Sort    []Sort
	Filters []Filter
	Cursor  string
}

// Limits is a read's paging policy: the size when a request names none, the
// largest it may name, and whether the read continues by cursor. A read that
// does not continue by cursor refuses one rather than serve the wrong page.
// [ParseQuery] panics unless 1 <= DefaultSize <= MaxSize.
type Limits struct {
	DefaultSize int
	MaxSize     int
	Cursor      bool
}

// ParseQuery parses a read request's query string into a [Query] under l,
// rejecting a malformed or out-of-bounds parameter with a *[QueryError].
func ParseQuery(q url.Values, l Limits) (Query, error) {
	if l.DefaultSize < 1 || l.MaxSize < l.DefaultSize {
		panic(fmt.Sprintf(
			"web: Limits{DefaultSize: %d, MaxSize: %d} is invalid: DefaultSize must be at least 1 and MaxSize at least DefaultSize",
			l.DefaultSize, l.MaxSize,
		))
	}

	out := Query{Page: 1, Size: l.DefaultSize}

	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Query{}, &QueryError{
				Param: "page", Value: v,
				Reason: "must be an integer of at least 1",
			}
		}
		// The data layer's offset, (page-1) times the size, must fit an int
		// at the largest size a request may ask for.
		if last := math.MaxInt / l.MaxSize; n > last {
			return Query{}, &QueryError{
				Param: "page", Value: v,
				Reason: fmt.Sprintf("must be at most %d", last),
			}
		}
		out.Page = n
	}

	if v := q.Get("cursor"); v != "" {
		if !l.Cursor {
			return Query{}, &QueryError{
				Param: "cursor", Value: v,
				Reason: "this read does not continue by cursor",
			}
		}
		if p := q.Get("page"); p != "" {
			return Query{}, &QueryError{
				Param: "cursor", Value: v,
				Reason: "cannot be combined with page",
			}
		}
		out.Page = 0
		out.Cursor = v
	}

	if v := q.Get("size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Query{}, &QueryError{
				Param: "size", Value: v,
				Reason: "must be an integer of at least 1",
			}
		}
		if n > l.MaxSize {
			return Query{}, &QueryError{
				Param: "size", Value: v,
				Reason: fmt.Sprintf("must be at most %d", l.MaxSize),
			}
		}
		out.Size = n
	}

	for _, v := range q["sort"] {
		if v == "" {
			continue
		}
		for _, token := range strings.Split(v, ",") {
			s := Sort{
				Field:      strings.TrimPrefix(token, "-"),
				Descending: strings.HasPrefix(token, "-"),
			}
			if s.Field == "" {
				return Query{}, &QueryError{
					Param: "sort", Value: v,
					Reason: "empty sort key",
				}
			}
			out.Sort = append(out.Sort, s)
		}
	}

	out.Filters = []Filter{}
	for key, values := range q {
		if _, ok := reserved[key]; ok {
			continue
		}
		field, op, err := parseFilterKey(key, values)
		if err != nil {
			return Query{}, err
		}
		out.Filters = append(out.Filters, Filter{Field: field, Op: op, Values: values})
	}
	slices.SortFunc(out.Filters, func(a, b Filter) int {
		if c := strings.Compare(a.Field, b.Field); c != 0 {
			return c
		}
		return strings.Compare(a.Op, b.Op)
	})

	return out, nil
}

// parseFilterKey splits a filter key into its field and operator, "name" or
// "name[op]", rejecting anything else with the parameter's first value.
func parseFilterKey(key string, values []string) (field, op string, err error) {
	value := ""
	if len(values) > 0 {
		value = values[0]
	}
	reject := func(param, reason string) (string, string, error) {
		return "", "", &QueryError{Param: param, Value: value, Reason: reason}
	}

	if key == "" {
		return reject(key, "the parameter has no name")
	}
	open := strings.IndexByte(key, '[')
	if open < 0 {
		if strings.IndexByte(key, ']') >= 0 {
			return reject(key, "unbalanced bracket in the parameter name")
		}
		return key, "", nil
	}
	if open == 0 {
		return reject(key, "the operator must follow a field name")
	}
	if !strings.HasSuffix(key, "]") || strings.Count(key, "[") != 1 || strings.Count(key, "]") != 1 {
		return reject(key, "the operator must be one bracketed suffix, like name[op]")
	}
	field, op = key[:open], key[open+1:len(key)-1]
	if op == "" {
		return reject(key, "empty operator")
	}
	if _, ok := reserved[field]; ok {
		return reject(field, "takes no operator")
	}
	return field, op, nil
}
