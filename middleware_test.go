package web_test

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/internal/webtest"
)

// tag returns middleware that appends its name to order as the request passes
// through, so the recorded sequence is the execution order.
func tag(order *[]string, name string) web.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, name)
			next.ServeHTTP(w, r)
		})
	}
}

func TestChain_RunsInArgumentOrder(t *testing.T) {
	var order []string
	handler := web.Chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			order = append(order, "handler")
		}),
		tag(&order, "first"),
		tag(&order, "second"),
	)

	webtest.Probe(handler, "/")

	want := []string{"first", "second", "handler"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestChain_NoMiddlewareReturnsTheHandler(t *testing.T) {
	var served bool
	handler := web.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		served = true
	}))

	webtest.Probe(handler, "/")

	if !served {
		t.Error("the handler did not run through an empty chain")
	}
}

func TestChain_SkipsNilMiddleware(t *testing.T) {
	var order []string
	handler := web.Chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			order = append(order, "handler")
		}),
		nil,
		tag(&order, "only"),
		nil,
	)

	webtest.Probe(handler, "/")

	if len(order) != 2 || order[0] != "only" || order[1] != "handler" {
		t.Errorf("order = %v, want [only handler]", order)
	}
}
