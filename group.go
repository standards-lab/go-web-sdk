package web

import (
	"net/http"
	"strings"
)

type route struct {
	method     string
	pattern    string
	handler    http.Handler
	middleware []Middleware
}

// Group declares routes under a path prefix, with a middleware stack and
// nested child groups. [NewModule] compiles and seals it; any mutation of a
// sealed group panics, so a route added after compilation is never silently
// dead.
type Group struct {
	prefix           string
	middleware       []Middleware
	routes           []route
	groups           []*Group
	errors           *ErrorWriter
	notFound         http.Handler
	methodNotAllowed http.Handler
	sealed           bool
}

// NewGroup returns an open group rooted at prefix, which may span segments
// ("/api/v1"). It panics on a prefix that does not begin with '/' or ends
// with one.
func NewGroup(prefix string) *Group {
	validatePrefix(prefix)
	return &Group{prefix: prefix}
}

// Use appends middleware wrapping every route in the group and its
// children.
func (g *Group) Use(mw ...Middleware) {
	g.checkSeal()
	g.middleware = append(g.middleware, mw...)
}

// Handle registers handler for method and pattern under the group's
// prefix, with mw innermost. An empty pattern binds the prefix itself, and
// an empty method matches every method.
func (g *Group) Handle(
	method, pattern string,
	handler http.Handler,
	mw ...Middleware,
) {
	g.checkSeal()
	g.routes = append(
		g.routes,
		route{
			method:     method,
			pattern:    pattern,
			handler:    handler,
			middleware: mw,
		},
	)
}

// SetErrorWriter sets the writer [Group.HandleErr] adapts the group's
// handlers with. A child group does not inherit it.
func (g *Group) SetErrorWriter(ew *ErrorWriter) {
	g.checkSeal()
	g.errors = ew
}

// SetNotFound replaces the module's 404 problem for a request under its
// prefix that matches no route; nil restores it. It belongs on the group
// passed to [NewModule], which panics on a nested group carrying one.
func (g *Group) SetNotFound(h http.Handler) {
	g.checkSeal()
	g.notFound = h
}

// SetMethodNotAllowed is [Group.SetNotFound] for a matching path with the
// wrong method; the Allow header is already set when h runs.
func (g *Group) SetMethodNotAllowed(h http.Handler) {
	g.checkSeal()
	g.methodNotAllowed = h
}

// HandleErr is [Group.Handle] for an error-returning handler, adapted by
// [Handle] with the group's writer. It panics on a group with no writer.
func (g *Group) HandleErr(
	method, pattern string,
	fn HandlerFunc,
	mw ...Middleware,
) {
	if g.errors == nil {
		panic("web: HandleErr on a group with no error writer; call SetErrorWriter first: " + g.prefix + pattern)
	}
	g.Handle(method, pattern, Handle(fn, g.errors), mw...)
}

// Mount nests child under the group: its prefix appends to the group's, and
// the group's middleware wraps its own.
func (g *Group) Mount(child *Group) {
	g.checkSeal()
	g.groups = append(g.groups, child)
}

func (g *Group) checkSeal() {
	if g.sealed {
		panic("web: group modified after NewModule")
	}
}

func validatePrefix(prefix string) {
	if !strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") {
		panic("web: prefix must begin with '/' and not end with '/': " + prefix)
	}
}
