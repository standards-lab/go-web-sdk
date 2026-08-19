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

// Group is a declarative route group: a path prefix, a middleware stack,
// atomic routes, and nested child groups. [NewModule] compiles a group tree
// into a servable [Module] and seals it; mutating a sealed group panics, so a
// route added after compilation cannot be silently dead.
type Group struct {
	prefix     string
	middleware []Middleware
	routes     []route
	groups     []*Group
	sealed     bool
}

// NewGroup returns an open group rooted at prefix. The prefix may span
// multiple segments ("/api/v1"); it must begin with '/' and not end with one,
// and a malformed prefix panics — a wiring error caught at registration.
func NewGroup(prefix string) *Group {
	validatePrefix(prefix)
	return &Group{prefix: prefix}
}

// Use appends middleware to the group's stack, wrapping every route in this
// group and its children. Use after NewModule panics.
func (g *Group) Use(mw ...Middleware) {
	g.checkSeal()
	g.middleware = append(g.middleware, mw...)
}

// Handle registers handler for method and pattern relative to the group's
// prefix. An empty pattern binds the prefix itself; an empty method registers
// the pattern for every method. Per-route middleware wraps innermost. Handle
// after NewModule panics.
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

// HandleFunc is [Group.Handle] for a handler function.
func (g *Group) HandleFunc(
	method, pattern string,
	handler http.HandlerFunc,
	mw ...Middleware,
) {
	g.Handle(method, pattern, handler, mw...)
}

// Mount nests child under the group: the child's prefix appends to the
// parent's, and the parent's middleware wraps the child's. Mount after
// NewModule panics.
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
