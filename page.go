package web

// Page is the success envelope of a paginated read: one page of items, the
// paging that produced it, and the total row count across all pages. It is
// the whole response body, written with [WriteJSON].
type Page[T any] struct {
	Items []T `json:"items"`
	Page  int `json:"page"`
	Size  int `json:"size"`
	Total int `json:"total"`
}

// NewPage assembles the envelope from a fulfilled read: the page's items, the
// query the read honored, and the total row count. Nil items become an
// empty slice, so an empty page marshals its items as [] rather than null.
func NewPage[T any](items []T, q Query, total int) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{
		Items: items,
		Page:  q.Page,
		Size:  q.Size,
		Total: total,
	}
}
