package web

import (
	"net/http"
	"slices"
	"strings"
)

// Router dispatches to mounted [Module] values by longest-prefix match on
// segment boundaries, falling back to a native http.ServeMux, where
// [Router.Handle] registers outside every module's middleware.
type Router struct {
	mux              *http.ServeMux
	modules          []*Module
	middleware       []Middleware
	handler          http.Handler
	notFound         http.Handler
	methodNotAllowed http.Handler
}

// NewRouter returns a router with no modules and an empty native mux.
func NewRouter() *Router {
	r := &Router{
		mux:              http.NewServeMux(),
		notFound:         defaultNotFound,
		methodNotAllowed: defaultMethodNotAllowed,
	}
	r.handler = http.HandlerFunc(r.dispatch)
	return r
}

// SetNotFound replaces the 404 problem for a request no module owns and no
// [Router.Handle] pattern matches; nil restores it.
func (r *Router) SetNotFound(h http.Handler) {
	r.notFound = orDefault(h, defaultNotFound)
}

// SetMethodNotAllowed is [Router.SetNotFound] for a matching path with the
// wrong method; the Allow header is already set when h runs.
func (r *Router) SetMethodNotAllowed(h http.Handler) {
	r.methodNotAllowed = orDefault(h, defaultMethodNotAllowed)
}

// Handle mirrors http.ServeMux.Handle on the native mux, so *Router
// satisfies [Mounter] and [RegisterHealth] mounts the probes directly.
func (r *Router) Handle(pattern string, handler http.Handler) {
	r.mux.Handle(pattern, handler)
}

// Use appends middleware around the whole dispatch, modules and native mux
// alike.
func (r *Router) Use(mw ...Middleware) {
	r.middleware = append(r.middleware, mw...)
	r.handler = Chain(http.HandlerFunc(r.dispatch), r.middleware...)
}

// Mount adds m to the dispatch set. It panics on a second module at the same
// prefix.
func (r *Router) Mount(m *Module) {
	for _, mounted := range r.modules {
		if mounted.prefix == m.prefix {
			panic("web: module already mounted at " + m.prefix)
		}
	}
	r.modules = append(r.modules, m)
	slices.SortFunc(r.modules, func(a, b *Module) int {
		return len(b.prefix) - len(a.prefix)
	})
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}

func (r *Router) dispatch(w http.ResponseWriter, req *http.Request) {
	for _, m := range r.modules {
		if matches(req.URL.Path, m.prefix) {
			m.ServeHTTP(w, req)
			return
		}
	}
	serveMux(w, req, r.mux, r.notFound, r.methodNotAllowed)
}

func matches(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	return len(path) == len(prefix) || path[len(prefix)] == '/'
}
