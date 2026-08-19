package web

import "net/http"

// Module is a compiled, prefix-mounted handler with its middleware baked in,
// dispatched by a [Router].
type Module struct {
	prefix  string
	handler http.Handler
}

// NewModule compiles the group tree into a Module in one pass: every route
// registers on an internal mux under its full pattern with its full chain —
// group middleware outermost, ordered root to leaf, then per-route middleware
// — and the tree is sealed against further mutation. Composition happens
// once, here; nothing recomposes per request. A duplicate or malformed
// pattern panics at registration, from the mux itself.
func NewModule(g *Group) *Module {
	mux := http.NewServeMux()
	compile(mux, "", nil, g)
	return &Module{
		prefix:  g.prefix,
		handler: mux,
	}
}

// NewHandlerModule mounts a raw handler under prefix — an embedded client
// application, a file server — with mw wrapped around it. The handler
// receives the request with the prefix stripped, via http.StripPrefix; this
// is the one place a module rewrites a request path.
func NewHandlerModule(
	prefix string,
	handler http.Handler,
	mw ...Middleware,
) *Module {
	validatePrefix(prefix)
	return &Module{
		prefix:  prefix,
		handler: http.StripPrefix(prefix, Chain(handler, mw...)),
	}
}

// Prefix reports the path prefix the module mounts at.
func (m *Module) Prefix() string {
	return m.prefix
}

// ServeHTTP implements http.Handler.
func (m *Module) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.handler.ServeHTTP(w, r)
}

func compile(mux *http.ServeMux, parent string, outer []Middleware, g *Group) {
	g.sealed = true
	prefix := parent + g.prefix
	chain := append(append([]Middleware{}, outer...), g.middleware...)

	for _, rt := range g.routes {
		pattern := prefix + rt.pattern
		if rt.method != "" {
			pattern = rt.method + " " + pattern
		}
		mw := append(append([]Middleware{}, chain...), rt.middleware...)
		mux.Handle(pattern, Chain(rt.handler, mw...))
	}
	for _, child := range g.groups {
		compile(mux, prefix, chain, child)
	}
}
