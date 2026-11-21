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

func (r *records) next(t *testing.T) string {
	t.Helper()
	return recvOrFail(t, r.lines, "a log record")
}

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

type service struct {
	log         *records
	graph       *graph.Graph
	lcConfig    *graph.Node[lifecycle.Config]
	logger      *graph.Node[*slog.Logger]
	participant *graph.Node[*participant]
	server      *graph.Node[*web.Server]
	fake        *participant

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

func (s *service) value() web.Service {
	return web.Service{
		Graph:           s.graph,
		LifecycleConfig: s.lcConfig,
		Logger:          s.logger,
		Server:          s.server,
		Roots:           []graph.Ref{s.participant},
	}
}

func (s *service) builtServer() *web.Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.built
}

func (s *service) run(ctx context.Context, stderr io.Writer) <-chan int {
	done := make(chan int, 1)
	svc := s.value()
	go func() { done <- svc.Run(ctx, stderr) }()
	return done
}

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

func TestService_BuildFailureOnStderr(t *testing.T) {
	var stderr bytes.Buffer
	s := newService(t)
	s.graph.Replace(s.logger, func(*graph.Scope) (*slog.Logger, error) {
		return nil, errors.New("logger unavailable")
	})

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

func TestService_PanicsWhenLoggerNotReached(t *testing.T) {
	s := newService(t)
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
