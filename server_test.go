package web_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

// failsafe bounds every wait for an event that should occur, so a broken server
// fails the test instead of hanging it.
const failsafe = 2 * time.Second

func recvOrFail[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(failsafe):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

// finalized fills the config's unset fields with defaults so NewServer, which
// expects a finalized config, can be handed explicit values — an explicit
// Port 0 survives Finalize and means an ephemeral port.
func finalized(t *testing.T, cfg web.Config) web.Config {
	t.Helper()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	return cfg
}

// startTestServer binds a server on an ephemeral port.
func startTestServer(t *testing.T, handler http.Handler) *web.Server {
	t.Helper()

	srv := web.NewServer(finalized(t, web.Config{Host: "127.0.0.1", Port: new(0)}), handler)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), failsafe)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv
}

// NewServer requires a finalized Config; the diagnostic panic names the fix
// instead of nil-dereferencing a tri-state pointer.
func TestNewServer_PanicsOnUnfinalizedConfig(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("NewServer with a zero Config did not panic")
		}
		if want := "web: Config not finalized: call Finalize before NewServer"; r != want {
			t.Fatalf("panic = %v, want %q", r, want)
		}
	}()
	web.NewServer(web.Config{}, http.NewServeMux())
}

func TestServer_AddrBeforeStartIsConfigured(t *testing.T) {
	srv := web.NewServer(finalized(t, web.Config{Host: "127.0.0.1", Port: new(8080)}), http.NewServeMux())
	if got, want := srv.Addr(), "127.0.0.1:8080"; got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
}

func TestServer_StartServesRequests(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		_ = web.WriteJSON(w, http.StatusOK, map[string]string{"status": "pong"})
	})

	srv := startTestServer(t, mux)

	if _, port, err := net.SplitHostPort(srv.Addr()); err != nil {
		t.Fatalf("Addr() = %q, want host:port: %v", srv.Addr(), err)
	} else if p, _ := strconv.Atoi(port); p == 0 {
		t.Fatal("Addr() still reports port 0 after Start bound the listener")
	}

	resp, err := http.Get("http://" + srv.Addr() + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServer_StartTwiceFails(t *testing.T) {
	srv := startTestServer(t, http.NewServeMux())

	if err := srv.Start(context.Background()); err == nil {
		t.Fatal("the second Start returned nil")
	}
}

// TestServer_StartReportsBindFailure is the defect this package exists to fix:
// binding on the calling goroutine means a taken port fails the caller instead
// of being logged by a goroutine nobody is watching.
func TestServer_StartReportsBindFailure(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy a port: %v", err)
	}
	defer func() { _ = occupied.Close() }()

	host, port, err := net.SplitHostPort(occupied.Addr().String())
	if err != nil {
		t.Fatalf("split %q: %v", occupied.Addr(), err)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("parse port %q: %v", port, err)
	}

	srv := web.NewServer(finalized(t, web.Config{Host: host, Port: new(p)}), http.NewServeMux())
	err = srv.Start(context.Background())
	if err == nil {
		t.Fatal("Start returned nil for an occupied port")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Errorf("error = %v, want it to mention listen", err)
	}
}

func TestServer_ShutdownWaitsForInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_ = web.WriteJSON(w, http.StatusOK, map[string]string{"status": "done"})
	})

	srv := startTestServer(t, mux)

	type response struct {
		status int
		err    error
	}
	done := make(chan response, 1)
	go func() {
		resp, err := http.Get("http://" + srv.Addr() + "/slow")
		if err != nil {
			done <- response{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)
		done <- response{status: resp.StatusCode}
	}()

	recvOrFail(t, started, "the handler to start")

	shutdown := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), failsafe)
		defer cancel()
		shutdown <- srv.Shutdown(ctx)
	}()

	// Shutdown must not return while the request is still being served.
	select {
	case err := <-shutdown:
		t.Fatalf("Shutdown returned (%v) while a request was in flight", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	got := recvOrFail(t, done, "the in-flight request to complete")
	if got.err != nil {
		t.Fatalf("in-flight request failed: %v", got.err)
	}
	if got.status != http.StatusOK {
		t.Errorf("status = %d, want 200 — the request was cut off", got.status)
	}
	if err := recvOrFail(t, shutdown, "Shutdown to return"); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestServer_ErrClosesAfterCleanShutdown(t *testing.T) {
	srv := startTestServer(t, http.NewServeMux())

	ctx, cancel := context.WithTimeout(context.Background(), failsafe)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// http.ErrServerClosed is the expected end of Serve, so it is swallowed and
	// the channel simply closes.
	select {
	case err, ok := <-srv.Err():
		if ok {
			t.Errorf("Err() delivered %v after a clean shutdown", err)
		}
	case <-time.After(failsafe):
		t.Fatal("Err() was not closed after shutdown")
	}
}

func TestServer_ShutdownBeforeStartLeavesServerUsable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		_ = web.WriteJSON(w, http.StatusOK, map[string]string{"status": "pong"})
	})
	srv := web.NewServer(finalized(t, web.Config{Host: "127.0.0.1", Port: new(0)}), mux)

	ctx, cancel := context.WithTimeout(context.Background(), failsafe)
	defer cancel()

	// A drain after a failed startup passes through a never-started server, so
	// Shutdown before Start is a no-op, not an error.
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown before Start = %v, want nil", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start after early Shutdown: %v", err)
	}

	resp, err := http.Get("http://" + srv.Addr() + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown after Start: %v", err)
	}
}

func TestServer_WiresReadHeaderTimeoutFromConfig(t *testing.T) {
	cfg := finalized(t, web.Config{
		Host:              "127.0.0.1",
		Port:              new(0),
		ReadHeaderTimeout: dur(100 * time.Millisecond),
	})
	srv := web.NewServer(cfg, http.NewServeMux())
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), failsafe)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial %s: %v", srv.Addr(), err)
	}
	defer func() { _ = conn.Close() }()

	// Send an unfinished header block and never complete it; the configured
	// ReadHeaderTimeout must close the connection well before the failsafe.
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: example.test\r\n")); err != nil {
		t.Fatalf("write partial request: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(failsafe)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	// EOF or a reset both mean the server closed the connection in time; only
	// the client-side deadline expiring means the timeout was not enforced.
	_, err = io.ReadAll(conn)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("the connection stayed open past the failsafe; ReadHeaderTimeout was not wired")
	}
}
