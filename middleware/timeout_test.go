package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// A handler that waits on its context sees the deadline expire as
// context.DeadlineExceeded.
func TestTimeout_HandlerObservesTheDeadline(t *testing.T) {
	var err error
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		err = r.Context().Err()
		w.WriteHeader(http.StatusGatewayTimeout)
	}), middleware.Timeout(10*time.Millisecond))

	rec := webtest.Probe(handler, "/orders/7")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("context error = %v, want context.DeadlineExceeded", err)
	}
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want the handler's own 504", rec.Code)
	}
}

// A handler that finishes in time runs under a live context that carries
// the deadline, and its response passes through untouched.
func TestTimeout_HandlerWithinTheDeadlineIsUnaffected(t *testing.T) {
	var (
		err         error
		hasDeadline bool
	)
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasDeadline = r.Context().Deadline()
		err = r.Context().Err()
		_ = web.WriteJSON(w, http.StatusCreated, map[string]string{"id": "7"})
	}), middleware.Timeout(time.Minute))

	rec := webtest.Probe(handler, "/orders/7")

	if !hasDeadline {
		t.Error("the handler's context has no deadline")
	}
	if err != nil {
		t.Errorf("context error = %v inside the deadline, want nil", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
}

// The timer is cancelled once the handler returns, so a request that
// finished in time does not hold its timer until the deadline.
func TestTimeout_CancelsAfterTheHandlerReturns(t *testing.T) {
	var ctx context.Context
	handler := web.Chain(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	}), middleware.Timeout(time.Minute))

	webtest.Probe(handler, "/orders/7")

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("context error after return = %v, want context.Canceled", ctx.Err())
	}
}

// Timeout is not http.TimeoutHandler: it writes no response of its own and
// does not race the handler, so a handler that runs past the deadline and
// answers anyway is the one the client hears from.
func TestTimeout_DoesNotWriteAResponse(t *testing.T) {
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		_ = web.WriteJSON(w, http.StatusOK, map[string]string{"late": "yes"})
	}), middleware.Timeout(10*time.Millisecond))

	rec := webtest.Probe(handler, "/orders/7")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the handler's own 200", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"late\":\"yes\"}\n" {
		t.Errorf("body = %q, want the handler's own body", got)
	}
}

func TestTimeout_NonPositiveDurationPanics(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		mustPanic(t, "Timeout("+d.String()+")", func() {
			middleware.Timeout(d)
		})
	}
}
