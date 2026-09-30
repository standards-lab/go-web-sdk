package web

import (
	"log/slog"
	"net/http"
	"testing"
)

// These tests read the wrapped http.Server directly: the wire-level
// behaviour (a 431 on an oversized header, net/http's own diagnostics in the
// slog pipeline) is proved in server_test.go, and what stays here is the
// exact field state the Config contract promises.

func internalConfig(t *testing.T, cfg Config) Config {
	t.Helper()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	return cfg
}

func TestNewServer_UnsetMaxHeaderBytesLeavesNetHTTPDefault(t *testing.T) {
	srv := NewServer(internalConfig(t, Config{Host: "127.0.0.1", Port: new(0)}), http.NewServeMux(), slog.New(slog.DiscardHandler))
	if got := srv.http.MaxHeaderBytes; got != 0 {
		t.Errorf("http.Server.MaxHeaderBytes = %d, want 0 (net/http applies DefaultMaxHeaderBytes itself)", got)
	}
}

func TestNewServer_ThreadsMaxHeaderBytes(t *testing.T) {
	cfg := internalConfig(t, Config{Host: "127.0.0.1", Port: new(0), MaxHeaderBytes: new(4096)})
	srv := NewServer(cfg, http.NewServeMux(), slog.New(slog.DiscardHandler))
	if got := srv.http.MaxHeaderBytes; got != 4096 {
		t.Errorf("http.Server.MaxHeaderBytes = %d, want 4096", got)
	}
}
