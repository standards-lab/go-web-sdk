package web

import "net/http"

// Module is a compiled, prefix-mounted handler with its middleware baked in,
// dispatched by a [Router].
type Module struct {
	prefix  string
	handler http.Handler
}

// NewModule compiles the group tree into a Module once, every route under its
// full pattern and chain, and seals the tree. It panics on a duplicate or
// malformed pattern. A miss under the prefix is answered by the root group's
// [Group.SetNotFound] and [Group.SetMethodNotAllowed] handlers.
func NewModule(g *Group) *Module {
	mux := http.NewServeMux()
	compile(mux, "", nil, g)
	notFound := orDefault(g.notFound, defaultNotFound)
	methodNotAllowed := orDefault(g.methodNotAllowed, defaultMethodNotAllowed)
	return &Module{
		prefix: g.prefix,
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serveMux(w, r, mux, notFound, methodNotAllowed)
		}),
	}
}

// NewHandlerModule mounts a raw handler under prefix, an embedded client
// application or a file server, with mw around it and the prefix stripped
// from the request: the one module that rewrites a request path.
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

// ServeHTTP implements http.Handler.
func (m *Module) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.handler.ServeHTTP(w, r)
}

func compile(mux *http.ServeMux, parent string, outer []Middleware, g *Group) {
	g.sealed = true
	prefix := parent + g.prefix
	// The miss handlers are the module's, read from the root group alone;
	// one set on a nested group would be dead wiring, so it panics like
	// every other registration mistake.
	if parent != "" && (g.notFound != nil || g.methodNotAllowed != nil) {
		panic("web: SetNotFound/SetMethodNotAllowed on a nested group; set them on the group passed to NewModule: " + prefix)
	}
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
