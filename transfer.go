package web

import (
	"fmt"
	"net/http"
	"time"
)

// Transfer is the time a route that moves a large body allows it: a body
// of up to limit bytes at no less than rate bytes per second, beside grace
// for the rest of the request. It exists because a server's read and write
// timeouts are tight, sized for a request that moves no large body, so an
// upload or a download widens its own connection's deadlines through
// http.ResponseController before it moves the body. A Transfer is built
// once at wiring, by [Config.Transfer] or [NewTransfer], and is safe for
// concurrent use.
type Transfer struct {
	limit int64
	rate  int64
	grace time.Duration
}

// NewTransfer returns the Transfer for a body of up to limit bytes at no
// less than rate bytes per second, with grace for the rest of the request.
// A negative limit or grace, or a rate that is not positive, is a wiring
// defect and panics.
func NewTransfer(limit, rate int64, grace time.Duration) Transfer {
	if limit < 0 || rate <= 0 || grace < 0 {
		panic(fmt.Sprintf("web: NewTransfer(%d, %d, %s): limit and grace must not be negative, and rate must be positive", limit, rate, grace))
	}
	return Transfer{limit: limit, rate: rate, grace: grace}
}

// Transfer returns the [Transfer] for a route whose body is at most limit
// bytes, at the configured TransferRate, with the longer of the read and
// write timeouts as its grace. It panics on an unfinalized Config.
func (c *Config) Transfer(limit int64) Transfer {
	if c.TransferRate == nil || c.ReadTimeout == nil || c.WriteTimeout == nil {
		panic("web: Config.Transfer on an unfinalized Config: call Finalize first")
	}
	return NewTransfer(limit, *c.TransferRate, max(c.ReadTimeout.Duration(), c.WriteTimeout.Duration()))
}

// Duration is how long the transfer allows: grace plus limit at rate.
func (t Transfer) Duration() time.Duration {
	return t.grace + time.Duration(float64(t.limit)/float64(t.rate)*float64(time.Second))
}

// Upload widens the connection's read and write deadlines to Duration from
// now, before the handler reads a request body of up to limit bytes: the
// read deadline covers the body, and the write deadline the response that
// follows it. A client slower than the rate fails the body's read with a
// deadline error (os.ErrDeadlineExceeded) once the deadline passes. It
// returns the http.ResponseController error for a writer that cannot set
// deadlines, which the handler reports as its failure.
func (t Transfer) Upload(w http.ResponseWriter) error {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(t.Duration())
	if err := rc.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("widen the read deadline: %w", err)
	}
	if err := rc.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("widen the write deadline: %w", err)
	}
	return nil
}

// Download widens the connection's write deadline to Duration from now,
// before the handler writes a response body of up to limit bytes. As
// Upload, it returns the http.ResponseController error for a writer that
// cannot set deadlines.
func (t Transfer) Download(w http.ResponseWriter) error {
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(t.Duration())); err != nil {
		return fmt.Errorf("widen the write deadline: %w", err)
	}
	return nil
}
