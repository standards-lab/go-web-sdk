package web_test

import (
	"bytes"
	"errors"
	"io"
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

	cfg = web.Config{TransferRate: new(int64(0))}
	if err := cfg.Finalize(""); err == nil || !strings.Contains(err.Error(), "transfer_rate") {
		t.Errorf("Finalize(transfer_rate 0) = %v, want it refused by name", err)
	}
}

// A Transfer allows grace plus the limit at the rate; Config.Transfer takes
// the longer of the read and write timeouts as its grace.
func TestTransfer_Duration(t *testing.T) {
	if d := web.NewTransfer(10<<20, 1<<20, 5*time.Second).Duration(); d != 15*time.Second {
		t.Errorf("Duration = %s, want 15s: 5s grace and 10 MiB at 1 MiB/s", d)
	}
	cfg := web.Config{ReadTimeout: dur(10 * time.Second), WriteTimeout: dur(20 * time.Second), TransferRate: new(int64(1 << 20))}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if d := cfg.Transfer(1 << 20).Duration(); d != 21*time.Second {
		t.Errorf("Config.Transfer Duration = %s, want 21s: the 20s write timeout and 1 MiB at 1 MiB/s", d)
	}
}

func TestNewTransfer_PanicsOnADefect(t *testing.T) {
	for name, build := range map[string]func(){
		"zero rate":      func() { web.NewTransfer(1, 0, 0) },
		"negative limit": func() { web.NewTransfer(-1, 1, 0) },
		"negative grace": func() { web.NewTransfer(1, 1, -time.Second) },
		"unfinalized":    func() { new(web.Config).Transfer(1) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			build()
		})
	}
}

// timedServer serves h with tight read and write timeouts, as a Config's
// defaults would set them, scaled down.
func timedServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ReadTimeout = 100 * time.Millisecond
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// pacedBody sends n bytes in chunks of size, pausing between chunks.
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

// Upload lets a client within the rate send a body for longer than the
// server's read timeout, and fails a client slower than the rate with a
// deadline error once the widened deadline passes.
func TestTransfer_Upload(t *testing.T) {
	transfer := web.NewTransfer(1000, 2000, 100*time.Millisecond) // 600ms
	read := make(chan error, 1)
	srv := timedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := transfer.Upload(w); err != nil {
			t.Errorf("Upload: %v", err)
		}
		_, err := io.Copy(io.Discard, r.Body)
		read <- err
		if err == nil {
			w.WriteHeader(http.StatusNoContent)
		}
	})

	// Within the rate: 1000 bytes over about 300ms, past the 100ms read
	// timeout.
	req, _ := http.NewRequest("POST", srv.URL, &pacedBody{left: 1000, size: 100, pause: 30 * time.Millisecond})
	req.ContentLength = 1000
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("paced upload: %v", err)
	}
	_ = resp.Body.Close()
	if err := <-read; err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("paced upload: read %v, status %d; want the whole body", err, resp.StatusCode)
	}

	// Slower than the rate: 1000 bytes over about 1s.
	req, _ = http.NewRequest("POST", srv.URL, &pacedBody{left: 1000, size: 100, pause: 100 * time.Millisecond})
	req.ContentLength = 1000
	if resp, err := srv.Client().Do(req); err == nil {
		_ = resp.Body.Close()
	}
	if err := <-read; !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("slow upload: read %v, want os.ErrDeadlineExceeded", err)
	}
}

// Download lets a handler write a response for longer than the server's
// write timeout.
func TestTransfer_Download(t *testing.T) {
	transfer := web.NewTransfer(1000, 2000, 100*time.Millisecond) // 600ms
	srv := timedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("widen") {
			if err := transfer.Download(w); err != nil {
				t.Errorf("Download: %v", err)
			}
		}
		for range 10 {
			time.Sleep(30 * time.Millisecond)
			_, _ = w.Write(bytes.Repeat([]byte("x"), 100))
			_ = http.NewResponseController(w).Flush()
		}
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

// A writer that cannot set deadlines reports the controller's error.
func TestTransfer_UnsupportedWriter(t *testing.T) {
	transfer := web.NewTransfer(1, 1, 0)
	if err := transfer.Upload(httptest.NewRecorder()); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Upload on a recorder = %v, want http.ErrNotSupported", err)
	}
	if err := transfer.Download(httptest.NewRecorder()); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Download on a recorder = %v, want http.ErrNotSupported", err)
	}
}
