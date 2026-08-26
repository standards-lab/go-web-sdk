package web

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Sort is one sort key parsed from a request: a field name and direction.
// The name is lexical only — whether it names a readable field is the data
// layer's check.
type Sort struct {
	Field      string
	Descending bool
}

// Directives is one read request's parsed paging directives. Page is 1-based
// and defaults to 1; Size is defaulted and capped by the [Limits] given to
// [ParseDirectives]; Sort preserves request order and is nil when the request
// carries no sort.
type Directives struct {
	Page int
	Size int
	Sort []Sort
}

// Limits bounds directive parsing: the page size when a request omits one,
// and the largest size a request may ask for. The package holds no policy
// numbers of its own; a consumer declares them here. DefaultSize must be at
// least 1 and MaxSize at least DefaultSize — [ParseDirectives] panics
// otherwise, since invalid limits are a wiring mistake, not request input.
type Limits struct {
	DefaultSize int
	MaxSize     int
}

// DirectiveError reports one rejected query parameter: which parameter
// ("page", "size", or "sort"), the offending input, and why. The consumer
// maps it to its own response; this package mints no problem types.
type DirectiveError struct {
	Param  string
	Value  string
	Reason string
}

func (e *DirectiveError) Error() string {
	return fmt.Sprintf("directive %s=%q: %s", e.Param, e.Value, e.Reason)
}

// ParseDirectives reads the page, size, and sort parameters of a query into
// [Directives] under the given limits. An absent page is 1 and an absent size
// is the default; sort is comma-separated field names, each optionally
// prefixed with "-" for descending, honored across every occurrence of the
// parameter in order. An empty parameter value reads as omitted. A malformed
// or out-of-bounds parameter is a *[DirectiveError].
func ParseDirectives(q url.Values, l Limits) (Directives, error) {
	if l.DefaultSize < 1 || l.MaxSize < l.DefaultSize {
		panic(fmt.Sprintf(
			"web: Limits{DefaultSize: %d, MaxSize: %d} is invalid: DefaultSize must be at least 1 and MaxSize at least DefaultSize",
			l.DefaultSize, l.MaxSize,
		))
	}

	d := Directives{Page: 1, Size: l.DefaultSize}

	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Directives{}, &DirectiveError{
				Param: "page", Value: v,
				Reason: "must be an integer of at least 1",
			}
		}
		d.Page = n
	}

	if v := q.Get("size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Directives{}, &DirectiveError{
				Param: "size", Value: v,
				Reason: "must be an integer of at least 1",
			}
		}
		if n > l.MaxSize {
			return Directives{}, &DirectiveError{
				Param: "size", Value: v,
				Reason: fmt.Sprintf("must be at most %d", l.MaxSize),
			}
		}
		d.Size = n
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
				return Directives{}, &DirectiveError{
					Param: "sort", Value: v,
					Reason: "empty sort key",
				}
			}
			d.Sort = append(d.Sort, s)
		}
	}

	return d, nil
}
