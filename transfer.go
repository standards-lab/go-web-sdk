package web

import (
	"fmt"
	"math"
	"net/http"
	"time"
)

// Transfer sets the connection deadlines of a route that moves a large
// body. A server's read and write timeouts are sized for a request that
// moves no large body, so an upload or a download sets its own deadlines
// through http.ResponseController before it moves the body: the grace,
// plus the body's size at the rate, the slowest pace a client is allowed.
// The size is the body's own, capped at the route's limit, so a small body
// on a route with a large limit gets a small deadline.
//
// A Transfer sets only the connection's deadlines, never the request's
// context: a route that moves a body must not sit under a
// middleware.Timeout shorter than the transfer it allows.
//
// A Transfer is built once at wiring, by [Config.Transfer] or
// [NewTransfer], and is safe for concurrent use. Its zero value is a
// wiring defect: every method panics on it.
type Transfer struct {
	limit       int64
	rate        int64
	grace       time.Duration
	read, write bool
}

// NewTransfer returns the Transfer for a route whose body is at most limit
// bytes, at no less than rate bytes per second, with grace for the rest of
// the request; it sets both deadlines. A negative limit or grace, or a
// rate that is not positive, is a wiring defect and panics.
func NewTransfer(limit, rate int64, grace time.Duration) Transfer {
	if limit < 0 || rate <= 0 || grace < 0 {
		panic(fmt.Sprintf("web: NewTransfer(%d, %d, %s): limit and grace must not be negative, and rate must be positive", limit, rate, grace))
	}
	return Transfer{limit: limit, rate: rate, grace: grace, read: true, write: true}
}

// Transfer returns the [Transfer] for a route whose body is at most limit
// bytes, at the configured TransferRate, with the longer of the read and
// write timeouts as its grace. A timeout the Config disables (an explicit
// zero) stays disabled: the Transfer leaves that deadline alone. It panics
// on an unfinalized Config, as [NewTransfer] does on a negative limit.
func (c *Config) Transfer(limit int64) Transfer {
	if !c.finalized() {
		panic("web: Config not finalized: call Finalize or FinalizeBlock before Transfer")
	}
	read, write := c.ReadTimeout.Duration(), c.WriteTimeout.Duration()
	t := NewTransfer(limit, *c.TransferRate, max(read, write))
	t.read, t.write = read > 0, write > 0
	return t
}

// Limit is the route's body limit, the one [ReadUpload] takes.
func (t Transfer) Limit() int64 {
	t.check()
	return t.limit
}

// Duration is how long the transfer allows a body of size bytes: the
// grace plus size, capped at the limit, at the rate. A negative size is
// the limit's. It saturates rather than overflow.
func (t Transfer) Duration(size int64) time.Duration {
	t.check()
	if size < 0 || size > t.limit {
		size = t.limit
	}
	d := float64(t.grace) + float64(size)/float64(t.rate)*float64(time.Second)
	if d >= math.MaxInt64 {
		return math.MaxInt64
	}
	return time.Duration(d)
}

// WidenUpload sets the connection's deadlines before the handler reads r's
// body: the read deadline to Duration of the declared Content-Length from
// now, and the write deadline a grace later, for the response that
// follows the body. A client slower than the rate fails the body's read
// with a deadline error (os.ErrDeadlineExceeded), and the response to it
// can still be written. It returns the http.ResponseController error for a
// writer that cannot set deadlines.
func (t Transfer) WidenUpload(w http.ResponseWriter, r *http.Request) error {
	read := deadline(t.Duration(r.ContentLength))
	rc := http.NewResponseController(w)
	if t.read {
		if err := rc.SetReadDeadline(read); err != nil {
			return fmt.Errorf("set the read deadline: %w", err)
		}
	}
	if t.write {
		write := read
		if !read.IsZero() {
			write = read.Add(t.grace)
		}
		if err := rc.SetWriteDeadline(write); err != nil {
			return fmt.Errorf("set the write deadline: %w", err)
		}
	}
	return nil
}

// WidenDownload sets the connection's write deadline to Duration of size
// from now, before the handler writes a response body of size bytes. As
// WidenUpload, it returns the http.ResponseController error for a writer
// that cannot set deadlines.
func (t Transfer) WidenDownload(w http.ResponseWriter, size int64) error {
	d := t.Duration(size)
	if !t.write {
		return nil
	}
	if err := http.NewResponseController(w).SetWriteDeadline(deadline(d)); err != nil {
		return fmt.Errorf("set the write deadline: %w", err)
	}
	return nil
}

// deadline is d from now, or no deadline when d saturated.
func deadline(d time.Duration) time.Time {
	if d == math.MaxInt64 {
		return time.Time{}
	}
	return time.Now().Add(d)
}

func (t Transfer) check() {
	if t.rate == 0 {
		panic("web: zero Transfer: build it with Config.Transfer or NewTransfer")
	}
}
