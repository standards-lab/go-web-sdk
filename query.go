package web

import (
	"fmt"
	"net/url"
	"slices"
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

// Filter is one filter parsed from a request: a field name, the operator
// the request named in brackets after it ("created[gte]=2026-01-01"), and
// every value the parameter carried. Op is empty when the request named
// none ("status=active"), the plain form a consumer reads as equality. Both
// the name and the operator are lexical — whether the field is readable and
// the operator is one the read model supports is the data layer's check —
// and what several values mean under one operator is the consumer's
// translation; the SDK enumerates no operators of its own.
type Filter struct {
	Field  string
	Op     string
	Values []string
}

// Query is one read request's parsed query string: the page, size, and sort
// parameters, and every remaining parameter as the filter set. Page is
// 1-based and defaults to 1; Size is defaulted and capped by the [Limits]
// given to [ParseQuery]; Sort preserves request order and is nil when the
// request carries no sort. Filters is never nil and is ordered by field
// name, then operator — url.Values carries no request order, and a fixed
// order lets a consumer compose a deterministic predicate from it.
type Query struct {
	Page    int
	Size    int
	Sort    []Sort
	Filters []Filter
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

// ParseQuery parses one read request's query string in full: the page, size,
// and sort parameters under the given limits, and every remaining parameter
// as the filter set. One call yields both halves, so a handler cannot parse
// the paging parameters and forget to strip them from the filters. An absent
// page is 1 and an absent size is the default. Sort is comma-separated field
// names, each optionally prefixed with "-" for descending, honored across
// every occurrence of the parameter in order; an empty parameter value reads
// as omitted.
//
// A filter parameter is a field name, optionally followed by an operator in
// brackets: "status=active" is the field alone, "created[gte]=2026-01-01"
// names an operator, and a repeated parameter carries several values under
// one [Filter]. The operator passes through as text. A key with unbalanced,
// misplaced, or repeated brackets, an empty name, or an empty operator is
// rejected, as is an operator on page, size, or sort. A malformed or
// out-of-bounds parameter is a *[QueryError].
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

// parseFilterKey splits a filter parameter's key into its field name and
// operator: "name" yields ("name", ""), "name[op]" yields ("name", "op").
// Anything else is a *QueryError on the key, with the parameter's first
// value as the offending input; a reserved name with an operator is
// rejected on the reserved parameter.
func parseFilterKey(key string, values []string) (field, op string, err error) {
	value := ""
	if len(values) > 0 {
		value = values[0]
	}
	reject := func(param, reason string) (string, string, error) {
		return "", "", &QueryError{Param: param, Value: value, Reason: reason}
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
