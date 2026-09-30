package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
)

// Server wraps an http.Server whose bind and serve phases are split, so a
// composition root declares [Server.Start] and [Server.Shutdown] as a
// lifecycle service's members and monitors [Server.Err].
type Server struct {
	http *http.Server
	errs chan error

	mu       sync.Mutex
	listener net.Listener
}

// NewServer builds a Server from a finalized [Config] and the handler it
// serves, with net/http's own diagnostics (a TLS handshake failure, a
// header-parse error) logged through logger at warn level. It panics on an
// unfinalized cfg or a nil logger.
func NewServer(cfg Config, handler http.Handler, logger *slog.Logger) *Server {
	if !cfg.finalized() {
		panic("web: Config not finalized: call Finalize before NewServer")
	}
	if logger == nil {
		panic("web: NewServer requires a *slog.Logger")
	}

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout.Duration(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout.Duration(),
		WriteTimeout:      cfg.WriteTimeout.Duration(),
		IdleTimeout:       cfg.IdleTimeout.Duration(),
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	// An unset MaxHeaderBytes stays zero, net/http's signal to apply its own
	// default.
	if cfg.MaxHeaderBytes != nil {
		srv.MaxHeaderBytes = *cfg.MaxHeaderBytes
	}

	return &Server{
		http: srv,
		errs: make(chan error, 1),
	}
}

// Start binds the listener on the calling goroutine: a bind failure is the
// returned error, and ctx bounds the bind. It then serves in the
// background. A second Start returns an error.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return errors.New("server already started")
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.http.Addr, err)
	}
	s.listener = listener

	go func() {
		defer close(s.errs)
		if err := s.http.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.errs <- fmt.Errorf("serve %s: %w", listener.Addr(), err)
		}
	}()

	return nil
}

// Addr reports the bound address once started (a configured port 0 reads
// back its assignment), and the configured address before that.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.http.Addr
}

// Err delivers a serve failure after a successful Start; http.ErrServerClosed
// is the expected end of a shutdown and is not reported. The channel closes
// when serving stops.
func (s *Server) Err() <-chan error {
	return s.errs
}

// Shutdown drains the server gracefully through http.Server.Shutdown. When
// ctx ends first, it closes the connections net/http still tracks and
// returns ctx's error; hijacked connections and handlers still running are
// left to the caller. Before a successful Start it is a no-op that leaves the
// server startable; once it has served, a Server is single-use.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	started := s.listener != nil
	s.mu.Unlock()

	if !started {
		return nil
	}
	err := s.http.Shutdown(ctx)
	if err != nil && errors.Is(err, ctx.Err()) {
		// Close reaches only the connections net/http tracks; it neither
		// waits for a running handler nor touches a hijacked connection.
		_ = s.http.Close()
	}
	return err
}
