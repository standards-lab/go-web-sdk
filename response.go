package web

import (
	"encoding/json"
	"net/http"
)

// JSONMediaType is the media type [WriteJSON] sets.
const JSONMediaType = "application/json"

// WriteJSON sends data as JSON with the given status.
func WriteJSON(
	w http.ResponseWriter,
	status int,
	data any,
) error {
	w.Header().Set("Content-Type", JSONMediaType)
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

// Page is the success envelope of a paginated read: one page of items, the
// address that produced it, the total row count across all pages when the
// read counted one, whether a further page exists, and the cursor that
// continues from this page. It is the whole response body, written with
// [WriteJSON].
//
// Page is the 1-based number of a read addressed by number and is omitted
// for a read addressed by cursor, which has no number. Total is omitted
// when the read did not count, so a client never mistakes an uncounted
// read for an empty collection. More reports whether a further page exists,
// and Next, when present, is the token a client sends as the cursor
// parameter to read it; a read whose ordering cannot be continued by cursor
// reports More with no Next, and the client pages by number instead.
type Page[T any] struct {
	Items []T    `json:"items"`
	Page  int    `json:"page,omitempty"`
	Size  int    `json:"size"`
	Total *int   `json:"total,omitempty"`
	More  bool   `json:"more"`
	Next  string `json:"next,omitempty"`
}

// Paging is what a fulfilled read reports beyond its items: the total row
// count, negative when the read did not count one, whether a further page
// exists, and the cursor that continues from the page, empty when there is
// none. A data layer's own collection type maps onto it field for field.
type Paging struct {
	Total int
	More  bool
	Next  string
}

// NewPage assembles the envelope from a fulfilled read: the page's items,
// the query the read honored, and what the read reported. Nil items become
// an empty slice, so an empty page marshals its items as [] rather than
// null, and a negative total becomes an absent one.
func NewPage[T any](items []T, q Query, p Paging) Page[T] {
	if items == nil {
		items = []T{}
	}
	page := Page[T]{
		Items: items,
		Page:  q.Page,
		Size:  q.Size,
		More:  p.More,
		Next:  p.Next,
	}
	if p.Total >= 0 {
		total := p.Total
		page.Total = &total
	}
	return page
}
