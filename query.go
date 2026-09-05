package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// reserved names the query parameters [ParseQuery] consumes itself; every
// other parameter passes through as a filter. No other layer knows this set.
var reserved = map[string]struct{}{
	"page": {},
	"size": {},
	"sort": {},
}

// Sort is one sort key parsed from a request: a field name and direction.
// The name is lexical only — whether it names a readable field is the data
// layer's check.
type Sort struct {
	Field      string
	Descending bool
}

// Query is one read request's parsed query string: the page, size, and sort
// parameters, and every remaining parameter as the filter set. Page is
// 1-based and defaults to 1; Size is defaulted and capped by the [Limits]
// given to [ParseQuery]; Sort preserves request order and is nil when the
// request carries no sort; Filters is never nil. Filter names are lexical
// here — validating them against the read model is the data layer's job.
type Query struct {
	Page    int
	Size    int
	Sort    []Sort
	Filters url.Values
}

// Limits bounds query parsing: the page size when a request omits one, and
// the largest size a request may ask for. The package holds no policy
// numbers of its own; a consumer declares them here. DefaultSize must be at
// least 1 and MaxSize at least DefaultSize — [ParseQuery] panics
// otherwise, since invalid limits are a wiring mistake, not request input.
type Limits struct {
	DefaultSize int
	MaxSize     int
}

// QueryError reports one rejected query parameter: which parameter
// ("page", "size", or "sort"), the offending input, and why. [ErrorWriter]
// maps it to a 400; this package mints no problem types.
type QueryError struct {
	Param  string
	Value  string
	Reason string
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("query %s=%q: %s", e.Param, e.Value, e.Reason)
}

func (e *QueryError) status() int { return http.StatusBadRequest }

// ParseQuery parses one read request's query string in full: the page, size,
// and sort parameters under the given limits, and every remaining parameter
// as the filter set. One call yields both halves, so a handler cannot parse
// the paging parameters and forget to strip them from the filters. An absent
// page is 1 and an absent size is the default; sort is comma-separated field
// names, each optionally prefixed with "-" for descending, honored across
// every occurrence of the parameter in order; an empty parameter value reads
// as omitted. A malformed or out-of-bounds parameter is a *[QueryError].
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
		out.Page = n
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

	out.Filters = url.Values{}
	for key, values := range q {
		if _, ok := reserved[key]; ok {
			continue
		}
		out.Filters[key] = values
	}

	return out, nil
}
