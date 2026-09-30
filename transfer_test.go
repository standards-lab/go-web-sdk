package web_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

// TransferRate defaults to 64 KiB/s, takes its override, and must be
// positive.
func TestConfig_TransferRate(t *testing.T) {
	var cfg web.Config
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if *cfg.TransferRate != 64<<10 {
		t.Errorf("TransferRate = %d, want 65536", *cfg.TransferRate)
	}

	t.Setenv(envTransferRate, "1024")
	cfg = web.Config{}
	if err := cfg.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if *cfg.TransferRate != 1024 {
		t.Errorf("TransferRate = %d, want the override's 1024", *cfg.TransferRate)
	}

	for _, rate := range []int64{0, -1} {
		cfg = web.Config{TransferRate: new(rate)}
		if err := cfg.Finalize(""); err == nil || !strings.Contains(err.Error(), "transfer_rate") {
			t.Errorf("Finalize(transfer_rate %d) = %v, want it refused by name", rate, err)
		}
	}
	t.Setenv(envTransferRate, "fast")
	cfg = web.Config{}
	if err := cfg.Finalize(testPrefix); err == nil || !strings.Contains(err.Error(), envTransferRate) {
		t.Errorf("Finalize(%s=fast) = %v, want it refused naming the variable", envTransferRate, err)
	}
}

// A Transfer allows grace plus the body's size, capped at the limit, at
// the rate; a negative size counts as the limit, and a transfer too long
// for a Duration saturates rather than overflowing. Config.Transfer takes the
// longer of the read and write timeouts as its grace.
func TestTransfer_Duration(t *testing.T) {
	tr := web.NewTransfer(10<<20, 1<<20, 5*time.Second)
	for _, tc := range []struct {
		size int64
		want time.Duration
	}{
		{10 << 20, 15 * time.Second},
		{1 << 20, 6 * time.Second},
		{0, 5 * time.Second},
		{-1, 15 * time.Second},
		{20 << 20, 15 * time.Second},
	} {
		if d := tr.Duration(tc.size); d != tc.want {
			t.Errorf("Duration(%d) = %s, want %s", tc.size, d, tc.want)
		}
	}
	for _, huge := range []web.Transfer{web.NewTransfer(10<<30, 1, 0), web.NewTransfer(math.MaxInt64, 64<<10, 30*time.Second)} {
		if d := huge.Duration(-1); d != math.MaxInt64 {
			t.Errorf("Duration of an overflowing transfer = %s, want it saturated", d)
		}
	}
	cfg := web.Config{ReadTimeout: dur(10 * time.Second), WriteTimeout: dur(20 * time.Second), TransferRate: new(int64(1 << 20))}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if d := cfg.Transfer(1 << 20).Duration(-1); d != 21*time.Second {
		t.Errorf("Config.Transfer Duration = %s, want 21s: the 20s write timeout and 1 MiB at 1 MiB/s", d)
	}
	if got := cfg.Transfer(1 << 20).Limit(); got != 1<<20 {
		t.Errorf("Limit = %d, want 1 MiB", got)
	}
}

func TestTransfer_PanicsOnADefect(t *testing.T) {
	for name, use := range map[string]func(){
		"zero rate":      func() { web.NewTransfer(1, 0, 0) },
		"negative limit": func() { web.NewTransfer(-1, 1, 0) },
		"negative grace": func() { web.NewTransfer(1, 1, -time.Second) },
		"unfinalized":    func() { new(web.Config).Transfer(1) },
		"zero value":     func() { _ = web.Transfer{}.WidenDownload(httptest.NewRecorder(), 1) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			use()
		})
	}
}

// timedServer serves h through web.Handle, as a service serves its routes,
// with tight read and write timeouts: a Config's defaults, scaled down.
func timedServer(t *testing.T, h web.HandlerFunc) *httptest.Server {
	t.Helper()
	ew := web.NewErrorWriter(slog.New(slog.DiscardHandler))
	srv := httptest.NewUnstartedServer(web.Handle(h, ew))
	srv.Config.ReadTimeout = 100 * time.Millisecond
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// pacedBody yields left bytes in chunks of size, pausing before each chunk.
type pacedBody struct {
	left, size int
	pause      time.Duration
}

func (p *pacedBody) Read(b []byte) (int, error) {
	if p.left == 0 {
		return 0, io.EOF
	}
	time.Sleep(p.pause)
	n := min(p.size, p.left, len(b))
	p.left -= n
	return n, nil
}

// WidenUpload lets a client within the rate send a body for longer than
// the server's read timeout. It fails a client slower than the rate when
// the deadline its declared size earns passes, not at the read timeout.
func TestTransfer_WidenUpload(t *testing.T) {
	transfer := web.NewTransfer(1<<20, 2000, 100*time.Millisecond) // 1000 bytes: 600ms
	type outcome struct {
		err     error
		elapsed time.Duration
	}
	read := make(chan outcome, 1)
	srv := timedServer(t, func(w http.ResponseWriter, r *http.Request) error {
		start := time.Now()
		if err := transfer.WidenUpload(w, r); err != nil {
			return err
		}
		_, err := io.Copy(io.Discard, r.Body)
		read <- outcome{err, time.Since(start)}
		if err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	post := func(pause time.Duration) outcome {
		req, _ := http.NewRequest("POST", srv.URL, &pacedBody{left: 1000, size: 100, pause: pause})
		req.ContentLength = 1000
		if resp, err := srv.Client().Do(req); err == nil {
			_ = resp.Body.Close()
		}
		return <-read
	}

	// Within the rate: 1000 bytes over about 300ms, past the 100ms read
	// timeout.
	if o := post(30 * time.Millisecond); o.err != nil {
		t.Fatalf("paced upload: read %v; want the whole body", o.err)
	}
	// Slower than the rate: 1000 bytes over about 1s, cut off at about
	// 600ms, the deadline 1000 bytes earn, not the 100ms read timeout.
	o := post(100 * time.Millisecond)
	if !errors.Is(o.err, os.ErrDeadlineExceeded) {
		t.Errorf("slow upload: read %v, want os.ErrDeadlineExceeded", o.err)
	}
	if o.elapsed < 450*time.Millisecond || o.elapsed > 900*time.Millisecond {
		t.Errorf("slow upload cut off after %s, want about 600ms", o.elapsed)
	}
}

// WidenDownload lets a handler write a response for longer than the
// server's write timeout.
func TestTransfer_WidenDownload(t *testing.T) {
	transfer := web.NewTransfer(1<<20, 2000, 100*time.Millisecond) // 1000 bytes: 600ms
	srv := timedServer(t, func(w http.ResponseWriter, r *http.Request) error {
		if r.URL.Query().Has("widen") {
			if err := transfer.WidenDownload(w, 1000); err != nil {
				return err
			}
		}
		for range 10 {
			time.Sleep(30 * time.Millisecond)
			_, _ = w.Write(bytes.Repeat([]byte("x"), 100))
			_ = http.NewResponseController(w).Flush()
		}
		return nil
	})
	get := func(query string) (int, error) {
		resp, err := srv.Client().Get(srv.URL + query)
		if err != nil {
			return 0, err
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		return len(b), err
	}
	if n, err := get("?widen"); err != nil || n != 1000 {
		t.Errorf("widened download = %d bytes, %v; want all 1000", n, err)
	}
	if n, err := get(""); err == nil && n == 1000 {
		t.Error("the unwidened download outlasted the write timeout; the test proves nothing")
	}
}

// A Config that disables a timeout gets a Transfer that leaves that
// deadline alone: an upload on a server with no read timeout runs past
// what its size earns.
func TestTransfer_KeepsADisabledTimeoutDisabled(t *testing.T) {
	cfg := web.Config{ReadTimeout: dur(0), WriteTimeout: dur(0), TransferRate: new(int64(1 << 30))}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	transfer := cfg.Transfer(1 << 20) // 1000 bytes earn about 1µs
	read := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := transfer.WidenUpload(w, r); err != nil {
			t.Errorf("WidenUpload: %v", err)
		}
		_, err := io.Copy(io.Discard, r.Body)
		read <- err
	}))
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest("POST", srv.URL, &pacedBody{left: 1000, size: 500, pause: 50 * time.Millisecond})
	req.ContentLength = 1000
	if resp, err := srv.Client().Do(req); err == nil {
		_ = resp.Body.Close()
	}
	if err := <-read; err != nil {
		t.Errorf("read = %v; want the whole body, with no deadline set", err)
	}
}

// A writer with no connection deadlines has none to set: a handler served
// to a recorder, as in a unit test, runs as it would on a connection.
func TestTransfer_AWriterWithNoDeadlines(t *testing.T) {
	transfer := web.NewTransfer(1, 1, 0)
	r := httptest.NewRequest("POST", "/", nil)
	if err := transfer.WidenUpload(httptest.NewRecorder(), r); err != nil {
		t.Errorf("WidenUpload on a recorder = %v, want nil", err)
	}
	if err := transfer.WidenDownload(httptest.NewRecorder(), 1); err != nil {
		t.Errorf("WidenDownload on a recorder = %v, want nil", err)
	}
}
