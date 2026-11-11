package web_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-web-sdk"
)

// records is the logger node's writer: each record the handler writes is
// one line, sent on a channel as it is written and kept for the whole log.
type records struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	lines chan string
}

func newRecords() *records { return &records{lines: make(chan string, 64)} }

func (r *records) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.buf.Write(p)
	r.mu.Unlock()
	r.lines <- string(p)
	return len(p), nil
}

func (r *records) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// next returns the next record logged, or fails the test.
func (r *records) next(t *testing.T) string {
	t.Helper()
	return recvOrFail(t, r.lines, "a log record")
}

// participant is a fake node value the Coordinator starts, watches, and
// shuts down: Start fails with startErr, Shutdown with stopErr, and a
// runtime failure arrives on errs.
type participant struct {
	startErr error
	stopErr  error
	errs     chan error
}

var (
	_ lifecycle.Subsystem = (*participant)(nil)
	_ lifecycle.Monitored = (*participant)(nil)
)

func (p *participant) Start(context.Context) error    { return p.startErr }
func (p *participant) Shutdown(context.Context) error { return p.stopErr }
func (p *participant) Err() <-chan error              { return p.errs }

// service is a small graph in a service's shape: a lifecycle config node, a
// logger node over a records writer, a participant node, and a real
// *web.Server on an ephemeral port that uses both. The participant is the
// only root, so the server and the logger reach the Build only because
// Server joins every Build.
type service struct {
	log         *records
	graph       *graph.Graph
	lcConfig    *graph.Node[lifecycle.Config]
	logger      *graph.Node[*slog.Logger]
	participant *graph.Node[*participant]
	server      *graph.Node[*web.Server]
	fake        *participant

	// built is the server node's value, set when the Build constructs it.
	mu    sync.Mutex
	built *web.Server
}

func newService(t *testing.T) *service {
	t.Helper()
	cfg := finalized(t, web.Config{Host: "127.0.0.1", Port: new(0)})

	s := &service{log: newRecords(), graph: graph.New(), fake: &participant{errs: make(chan error, 1)}}
	s.lcConfig = s.graph.Value("lifecycle config", lifecycle.Config{ShutdownTimeout: config.Duration(failsafe)})
	s.logger = s.graph.Define("logger", func(*graph.Scope) (*slog.Logger, error) {
		return slog.New(slog.NewTextHandler(s.log, nil)), nil
	})
	s.participant = s.graph.Value("participant", s.fake)
	s.server = s.graph.Define("server", func(sc *graph.Scope) (*web.Server, error) {
		sc.Use(s.participant)
		srv := web.NewServer(cfg, http.NewServeMux(), sc.Use(s.logger))
		s.mu.Lock()
		s.built = srv
		s.mu.Unlock()
		return srv, nil
	})
	return s
}

// value returns the Service over s's graph.
func (s *service) value() web.Service {
	return web.Service{
		Graph:           s.graph,
		LifecycleConfig: s.lcConfig,
		Logger:          s.logger,
		Server:          s.server,
		Roots:           []graph.Ref{s.participant},
	}
}

// builtServer returns the server node's value, nil when no Build reached it.
func (s *service) builtServer() *web.Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.built
}

// run starts Run on s in a goroutine and returns the channel its exit code
// arrives on.
func (s *service) run(ctx context.Context, stderr io.Writer) <-chan int {
	done := make(chan int, 1)
	svc := s.value()
	go func() { done <- svc.Run(ctx, stderr) }()
	return done
}

// Once startup completes, Run logs "server ready" once with the address the
// server bound, the assigned port of a port 0; when the context ends and the
// System drains cleanly, it logs "server stopped" and returns 0. Nothing goes
// to stderr.
func TestService_ReadyThenCleanStop(t *testing.T) {
	var stderr bytes.Buffer
	s := newService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := s.run(ctx, &stderr)

	ready := s.log.next(t)
	addr := s.builtServer().Addr()
	if strings.HasSuffix(addr, ":0") {
		t.Fatalf("server Addr = %q after ready, want an assigned port", addr)
	}
	if !strings.Contains(ready, `msg="server ready"`) || !strings.Contains(ready, "addr="+addr) {
		t.Errorf("first record = %q, want server ready with addr=%s", ready, addr)
	}
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET on the ready server: %v", err)
	}
	_ = resp.Body.Close()

	cancel()
	if code := recvOrFail(t, done, "Run to return"); code != 0 {
		t.Errorf("Run = %d, want 0 on a clean drain", code)
	}
	if out := s.log.String(); strings.Count(out, "server ready") != 1 || !strings.Contains(out, `msg="server stopped"`) {
		t.Errorf("log = %q, want one ready record and a stopped record", out)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
}

// A failed startup logs "service failed" with an error naming the failing
// node and returns 1. Nothing reports ready.
func TestService_StartupFailureExitsOne(t *testing.T) {
	var stderr bytes.Buffer
	s := newService(t)
	s.fake.startErr = errors.New("bind refused")

	if code := recvOrFail(t, s.run(context.Background(), &stderr), "Run to return"); code != 1 {
		t.Errorf("Run = %d, want 1 on a failed startup", code)
	}
	out := s.log.String()
	if !strings.Contains(out, `msg="service failed"`) || !strings.Contains(out, "startup: participant: bind refused") {
		t.Errorf("log = %q, want a service failure naming the participant node and its error", out)
	}
	if strings.Contains(out, "server ready") {
		t.Errorf("log = %q, want no ready record", out)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
}

// A Monitored value's runtime failure ends the run: Run logs "service
// failed" with the error, naming the failing node, and returns 1 after the
// ready record.
func TestService_RuntimeFailureExitsOne(t *testing.T) {
	var stderr bytes.Buffer
	s := newService(t)
	done := s.run(context.Background(), &stderr)

	if ready := s.log.next(t); !strings.Contains(ready, `msg="server ready"`) {
		t.Fatalf("first record = %q, want server ready", ready)
	}
	s.fake.errs <- errors.New("connection lost")

	if code := recvOrFail(t, done, "Run to return"); code != 1 {
		t.Errorf("Run = %d, want 1 on a runtime failure", code)
	}
	out := s.log.String()
	if !strings.Contains(out, `msg="service failed"`) || !strings.Contains(out, "run: participant: connection lost") {
		t.Errorf("log = %q, want a service failure naming the participant node and its error", out)
	}
	if strings.Contains(out, "server stopped") {
		t.Errorf("log = %q, want no stopped record after a failure", out)
	}
}

// A failed shutdown fails the run: after a ready record and the end of the
// context, Run logs "service failed" with the error, naming the failing
// node, and returns 1.
func TestService_ShutdownFailureExitsOne(t *testing.T) {
	var stderr bytes.Buffer
	s := newService(t)
	s.fake.stopErr = errors.New("flush failed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := s.run(ctx, &stderr)

	if ready := s.log.next(t); !strings.Contains(ready, `msg="server ready"`) {
		t.Fatalf("first record = %q, want server ready", ready)
	}
	cancel()

	if code := recvOrFail(t, done, "Run to return"); code != 1 {
		t.Errorf("Run = %d, want 1 on a failed shutdown", code)
	}
	out := s.log.String()
	if !strings.Contains(out, `msg="service failed"`) || !strings.Contains(out, "shutdown: participant: flush failed") {
		t.Errorf("log = %q, want a service failure naming the participant node and its error", out)
	}
	if strings.Contains(out, "server stopped") {
		t.Errorf("log = %q, want no stopped record after a failure", out)
	}
}

// A constructor's error fails the Build: Run writes "service failed:" and
// the error, naming the failing node, to stderr and returns 1. It logs
// nothing, since the node that failed may be the logger, and starts
// nothing, so nothing reports ready.
func TestService_BuildFailureOnStderr(t *testing.T) {
	var stderr bytes.Buffer
	s := newService(t)
	s.graph.Replace(s.logger, func(*graph.Scope) (*slog.Logger, error) {
		return nil, errors.New("logger unavailable")
	})

	// Cancelled up front, so a Build that wrongly succeeds drains at once
	// and fails the exit-code check instead of hanging.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := recvOrFail(t, s.run(ctx, &stderr), "Run to return"); code != 1 {
		t.Errorf("Run = %d, want 1 on a failed Build", code)
	}
	if got, want := stderr.String(), "service failed: logger: logger unavailable\n"; got != want {
		t.Errorf("stderr = %q, want %q, naming the logger node and its error", got, want)
	}
	if out := s.log.String(); out != "" {
		t.Errorf("log = %q, want nothing", out)
	}
	if s.builtServer() != nil {
		t.Error("the server was constructed despite the failed Build")
	}
}

// panicsWith calls fn and fails the test unless it panics with a message
// containing want.
func panicsWith(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("did not panic, want a panic containing %q", want)
		}
		msg, ok := r.(string)
		if !ok || !strings.HasPrefix(msg, "web: ") || !strings.Contains(msg, want) {
			t.Fatalf("panic = %v, want a web: message containing %q", r, want)
		}
	}()
	fn()
}

// Run panics on a wiring mistake, naming the missing field, before it
// builds or starts anything.
func TestService_PanicsOnMissingField(t *testing.T) {
	tests := []struct {
		name  string
		want  string
		unset func(*web.Service)
	}{
		{"Graph", "Graph", func(s *web.Service) { s.Graph = nil }},
		{"LifecycleConfig", "LifecycleConfig", func(s *web.Service) { s.LifecycleConfig = nil }},
		{"Logger", "Logger", func(s *web.Service) { s.Logger = nil }},
		{"Server", "Server", func(s *web.Service) { s.Server = nil }},
		{"Roots", "Roots", func(s *web.Service) { s.Roots = nil }},
		{"EmptyRoots", "Roots", func(s *web.Service) { s.Roots = []graph.Ref{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newService(t)
			svc := s.value()
			tt.unset(&svc)
			var stderr bytes.Buffer
			panicsWith(t, tt.want, func() { svc.Run(context.Background(), &stderr) })
			if s.builtServer() != nil {
				t.Error("the Build ran despite the wiring mistake")
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want nothing", stderr.String())
			}
		})
	}
}

// A Build that does not reach the Logger node is a wiring mistake: Run
// panics, naming the Logger, before anything starts.
func TestService_PanicsWhenLoggerNotReached(t *testing.T) {
	s := newService(t)
	// The server logs to its own sink, so nothing the Build reaches uses
	// the logger node.
	s.graph.Replace(s.server, func(sc *graph.Scope) (*web.Server, error) {
		sc.Use(s.participant)
		srv := web.NewServer(finalized(t, web.Config{Host: "127.0.0.1", Port: new(0)}), http.NewServeMux(), discard)
		s.mu.Lock()
		s.built = srv
		s.mu.Unlock()
		return srv, nil
	})

	panicsWith(t, "Logger", func() { s.value().Run(context.Background(), io.Discard) })
	srv := s.builtServer()
	if srv == nil {
		t.Fatal("the server was not constructed: the panic came before the Build")
	}
	if addr := srv.Addr(); !strings.HasSuffix(addr, ":0") {
		t.Errorf("server Addr = %q, want the configured port 0: the server started", addr)
	}
}
