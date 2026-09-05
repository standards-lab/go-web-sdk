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
	errors     *ErrorWriter
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

// SetErrorWriter sets the writer the group's [Group.HandleErr] routes are
// adapted with. The writer is group-scoped, not per route: a layer builds
// one writer carrying its error vocabulary and every handler in the group
// returns errors through it. A child group does not inherit its parent's
// writer; each group that registers error-returning handlers sets its own.
// SetErrorWriter after NewModule panics.
func (g *Group) SetErrorWriter(ew *ErrorWriter) {
	g.checkSeal()
	g.errors = ew
}

// HandleErr registers an error-returning handler for method and pattern,
// adapted through the group's error writer by [Handle]; the pattern, method,
// and middleware rules are [Group.Handle]'s. A group with no writer panics
// with the fix named — a registration-time failure, like every other wiring
// mistake here. HandleErr after NewModule panics.
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
