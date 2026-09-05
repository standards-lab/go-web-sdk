package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/internal/webtest"
)

// mustPanic runs fn and fails the test unless it panics.
func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", name)
		}
	}()
	fn()
}

// ok is a handler that answers 200 with no body.
func ok() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestNewGroup_AcceptsMultiSegmentPrefix(t *testing.T) {
	g := web.NewGroup("/api/v1")
	g.Handle(http.MethodGet, "/orders", ok())

	rec := webtest.Probe(web.NewModule(g), "/api/v1/orders")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/v1/orders = %d, want 200", rec.Code)
	}
}

func TestNewGroup_MalformedPrefixPanics(t *testing.T) {
	for _, prefix := range []string{"", "api", "/api/", "/"} {
		mustPanic(t, "NewGroup("+prefix+")", func() {
			web.NewGroup(prefix)
		})
	}
}

// An empty pattern binds the prefix itself, so a group can answer at its own
// root without inventing a sub-path.
func TestGroup_EmptyPatternBindsThePrefix(t *testing.T) {
	g := web.NewGroup("/api/v1")
	g.Handle(http.MethodGet, "", ok())

	rec := webtest.Probe(web.NewModule(g), "/api/v1")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/v1 = %d, want 200", rec.Code)
	}
}

func TestGroup_EmptyMethodMatchesAllMethods(t *testing.T) {
	g := web.NewGroup("/api")
	g.Handle("", "/things", ok())
	m := web.NewModule(g)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(method, "/api/things", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s /api/things = %d, want 200", method, rec.Code)
		}
	}
}

// Compilation seals the whole tree, children included: a route registered
// after NewModule would be silently dead, so the mutation panics instead.
func TestGroup_SealedMutationPanics(t *testing.T) {
	parent := web.NewGroup("/api")
	child := web.NewGroup("/orders")
	parent.Mount(child)
	web.NewModule(parent)

	mustPanic(t, "Use after NewModule", func() {
		parent.Use(tag(&[]string{}, "late"))
	})
	mustPanic(t, "Handle after NewModule", func() {
		parent.Handle(http.MethodGet, "/late", ok())
	})
	mustPanic(t, "Mount after NewModule", func() {
		parent.Mount(web.NewGroup("/late"))
	})
	mustPanic(t, "Handle on a sealed child", func() {
		child.Handle(http.MethodGet, "/late", ok())
	})
}

func TestGroup_HandleErrWithoutAWriterPanics(t *testing.T) {
	g := web.NewGroup("/api")
	mustPanic(t, "HandleErr without SetErrorWriter", func() {
		g.HandleErr(http.MethodGet, "/things", func(http.ResponseWriter, *http.Request) error { return nil })
	})
}

func TestGroup_ErrorWriterIsNotInherited(t *testing.T) {
	parent := web.NewGroup("/api")
	parent.SetErrorWriter(web.NewErrorWriter())
	child := web.NewGroup("/things")
	parent.Mount(child)

	mustPanic(t, "HandleErr on a child without its own writer", func() {
		child.HandleErr(http.MethodGet, "", func(http.ResponseWriter, *http.Request) error { return nil })
	})
}

func TestGroup_SetErrorWriterAfterNewModulePanics(t *testing.T) {
	g := web.NewGroup("/api")
	web.NewModule(g)

	mustPanic(t, "SetErrorWriter after NewModule", func() {
		g.SetErrorWriter(web.NewErrorWriter())
	})
	mustPanic(t, "HandleErr after NewModule", func() {
		g.HandleErr(http.MethodGet, "", func(http.ResponseWriter, *http.Request) error { return nil })
	})
}
