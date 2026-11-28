package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/standards-lab/go-core/sse"
)

// See docs/requests-and-responses.md

const defaultKeepAlive = 15 * time.Second

var errStreamEnded = errors.New("web: event stream ended")

// EventStreamOptions configures an [EventStream]. Its zero value selects
// every default.
type EventStreamOptions struct {
	// KeepAlive is how long a stream may go without a write before the
	// writer sends a keepalive. Zero means 15 seconds; a negative value is
	// a wiring defect and panics.
	KeepAlive time.Duration
}

// EventStream serves a server-sent event stream: a response that stays open
// while the handler sends events through an [EventSender]. Before each
// write, the headers included, it sets the connection's write deadline to
// the Config's WriteTimeout from now, so a stream lasts past WriteTimeout
// while a client that stalls on one write fails within it. A WriteTimeout
// the Config disables (an explicit zero) leaves the deadline alone.
//
// A stream ends when its request's context ends or when the run context a
// [Server] carries into the request ends, whichever comes first, so a
// stream does not hold its connection past the start of a drain. An
// EventStream sets only the connection's write deadline, never the
// request's context: a stream route must not sit under a
// middleware.Timeout, whose duration would end the stream.
//
// An EventStream is built once at wiring, by [Config.EventStream], and is
// safe for concurrent use. Its zero value is a wiring defect: every method
// panics on it.
type EventStream struct {
	write     time.Duration
	keepAlive time.Duration
}

// EventStream returns the [EventStream] that uses the Config's
// WriteTimeout as each write's deadline. It panics on an unfinalized Config
// and on a negative opts.KeepAlive.
func (c *Config) EventStream(opts EventStreamOptions) EventStream {
	if !c.finalized() {
		panic("web: Config not finalized: call Finalize or FinalizeBlock before EventStream")
	}
	if opts.KeepAlive < 0 {
		panic(fmt.Sprintf("web: EventStream: KeepAlive %s must not be negative", opts.KeepAlive))
	}
	keepAlive := opts.KeepAlive
	if keepAlive == 0 {
		keepAlive = defaultKeepAlive
	}
	return EventStream{write: c.WriteTimeout.Duration(), keepAlive: keepAlive}
}

// Serve answers r with an event stream. It sets Content-Type to
// text/event-stream and Cache-Control to no-store, writes the 200 status,
// and flushes the headers, then calls fn with the stream's context and the
// [EventSender] fn writes through. The context ends when r's context or the
// run context a [Server] carries into r ends; a request served without a
// Server, as under a plain httptest server, ends with r alone.
//
// Serve returns fn's error and writes nothing once fn returns: no closing
// event, comment, or retry. When w cannot flush, or the headers cannot be
// written, Serve returns that error and never calls fn.
func (es EventStream) Serve(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, s *EventSender) error) error {
	es.check()
	ctx, cancel := streamContext(r.Context())
	defer cancel()

	out := &streamWriter{w: w}
	s := &EventSender{ctx: ctx, rc: http.NewResponseController(w), write: es.write, out: out, enc: sse.NewEncoder(out)}
	if err := s.extend(); err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := s.rc.Flush(); err != nil {
		return fmt.Errorf("flush the event stream's headers: %w", err)
	}

	err := fn(ctx, s)
	cancel()
	s.end()
	return err
}

func streamContext(req context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(req)
	run, ok := runContext(req)
	if !ok {
		return ctx, func() { cancel(nil) }
	}
	stop := context.AfterFunc(run, func() { cancel(context.Cause(run)) })
	return ctx, func() {
		stop()
		cancel(nil)
	}
}

func (es EventStream) check() {
	if es.keepAlive == 0 {
		panic("web: zero EventStream: build it with Config.EventStream")
	}
}

// EventSender writes the events of one stream [EventStream.Serve] serves,
// flushing each write. It is safe for concurrent use. Once the stream's
// context ends, a write fails, or Serve returns, the sender is closed: every
// later [EventSender.Send] and [EventSender.Retry] returns an error and
// writes nothing, so a producer's loop ends.
type EventSender struct {
	ctx   context.Context
	rc    *http.ResponseController
	write time.Duration

	mu     sync.Mutex
	out    *streamWriter
	enc    *sse.Encoder
	closed error
}

// Send writes e as one event and flushes it. When the sse.Encoder rejects
// e, Send returns its error, writes nothing, and leaves the stream open. A
// deadline, write, or flush error is returned and closes the sender.
func (s *EventSender) Send(e sse.Event) error {
	return s.do(func(enc *sse.Encoder) error { return enc.Encode(e) })
}

// Retry writes a retry field holding d, the reconnection time a client
// waits before it reconnects, and flushes it. A negative d is rejected as
// [EventSender.Send] rejects an event: an error, nothing written, and the
// stream left open.
func (s *EventSender) Retry(d time.Duration) error {
	return s.do(func(enc *sse.Encoder) error { return enc.Retry(d) })
}

func (s *EventSender) do(encode func(*sse.Encoder) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed != nil {
		return s.closed
	}
	if s.ctx.Err() != nil {
		s.closed = fmt.Errorf("%w: %w", errStreamEnded, context.Cause(s.ctx))
		return s.closed
	}
	if err := s.extend(); err != nil {
		s.closed = err
		return err
	}
	if err := encode(s.enc); err != nil {
		if s.out.err != nil {
			s.closed = fmt.Errorf("write the event stream: %w", err)
			return s.closed
		}
		return err
	}
	if err := s.rc.Flush(); err != nil {
		s.closed = fmt.Errorf("flush the event stream: %w", err)
		return s.closed
	}
	return nil
}

func (s *EventSender) extend() error {
	if s.write <= 0 {
		return nil
	}
	if err := s.rc.SetWriteDeadline(time.Now().Add(s.write)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("set the write deadline: %w", err)
	}
	return nil
}

type streamWriter struct {
	w   io.Writer
	err error
}

func (sw *streamWriter) Write(p []byte) (int, error) {
	n, err := sw.w.Write(p)
	if err != nil {
		sw.err = err
	}
	return n, err
}

func (s *EventSender) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed == nil {
		s.closed = errStreamEnded
	}
}
