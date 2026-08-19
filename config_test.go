package web_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-sdk"
)

// Instantiating Load proves *web.Config satisfies the config.Config contract at
// compile time; the constraint cannot be written as an ordinary interface
// assertion because it carries a type element.
var _ = config.Load[web.Config]

// testPrefix is the env prefix override tests finalize with; the names below
// are what Finalize composes from it.
const (
	testPrefix           = "test"
	envHost              = "TEST_SERVER_HOST"
	envPort              = "TEST_SERVER_PORT"
	envReadTimeout       = "TEST_SERVER_READ_TIMEOUT"
	envReadHeaderTimeout = "TEST_SERVER_READ_HEADER_TIMEOUT"
	envWriteTimeout      = "TEST_SERVER_WRITE_TIMEOUT"
	envIdleTimeout       = "TEST_SERVER_IDLE_TIMEOUT"
)

func dur(v time.Duration) *config.Duration {
	d := config.Duration(v)
	return &d
}

func TestConfig_MergeSourceWinsWithoutClearing(t *testing.T) {
	base := web.Config{
		Host:        "base",
		Port:        new(3000),
		ReadTimeout: dur(time.Minute),
	}
	base.Merge(&web.Config{Port: new(9000)})

	if base.Host != "base" {
		t.Errorf("Host = %q, want base (the source omits it and must not clear it)", base.Host)
	}
	if *base.Port != 9000 {
		t.Errorf("Port = %d, want 9000 (the source sets it)", *base.Port)
	}
	if time.Duration(*base.ReadTimeout) != time.Minute {
		t.Errorf("ReadTimeout = %s, want 1m (the source omits it)", base.ReadTimeout)
	}
}

func TestConfig_FinalizeAppliesDefaults(t *testing.T) {
	var cfg web.Config
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if cfg.Host != "0.0.0.0" || *cfg.Port != 8080 {
		t.Errorf("Addr() = %q, want 0.0.0.0:8080", cfg.Addr())
	}
	for _, want := range []struct {
		name  string
		got   *config.Duration
		value time.Duration
	}{
		{"ReadTimeout", cfg.ReadTimeout, time.Minute},
		{"ReadHeaderTimeout", cfg.ReadHeaderTimeout, 5 * time.Second},
		{"WriteTimeout", cfg.WriteTimeout, 15 * time.Minute},
		{"IdleTimeout", cfg.IdleTimeout, 2 * time.Minute},
	} {
		if want.got == nil {
			t.Errorf("%s = nil, want %s", want.name, want.value)
			continue
		}
		if time.Duration(*want.got) != want.value {
			t.Errorf("%s = %s, want %s", want.name, want.got, want.value)
		}
	}
}

func TestConfig_FinalizeEnvOverridesFiles(t *testing.T) {
	t.Setenv(envHost, "from-env")
	t.Setenv(envPort, "9443")
	t.Setenv(envReadTimeout, "45s")

	cfg := web.Config{Host: "from-file", Port: new(8080)}
	if err := cfg.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if cfg.Host != "from-env" {
		t.Errorf("Host = %q, want from-env", cfg.Host)
	}
	if *cfg.Port != 9443 {
		t.Errorf("Port = %d, want 9443", *cfg.Port)
	}
	if time.Duration(*cfg.ReadTimeout) != 45*time.Second {
		t.Errorf("ReadTimeout = %s, want 45s", cfg.ReadTimeout)
	}
	if time.Duration(*cfg.WriteTimeout) != 15*time.Minute {
		t.Errorf("WriteTimeout = %s, want the default (no override set)", cfg.WriteTimeout)
	}
}

func TestConfig_FinalizeEmptyEnvValueLeavesConfigured(t *testing.T) {
	// An empty variable reads as unset, not as a request to clear the value.
	t.Setenv(envReadTimeout, "")

	cfg := web.Config{ReadTimeout: dur(30 * time.Second)}
	if err := cfg.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if time.Duration(*cfg.ReadTimeout) != 30*time.Second {
		t.Errorf("ReadTimeout = %s, want 30s", cfg.ReadTimeout)
	}
}

func TestConfig_FinalizeEmptyPrefixSkipsOverrides(t *testing.T) {
	// An empty prefix composes no variable names, so nothing in the
	// environment applies — the hermetic form.
	t.Setenv(envHost, "from-env")
	t.Setenv(envPort, "9443")

	var cfg web.Config
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if cfg.Host != "0.0.0.0" || *cfg.Port != 8080 {
		t.Errorf("Addr() = %q, want the defaults with a zero Env", cfg.Addr())
	}
}

func TestConfig_FinalizeMalformedDurationNamesTheVariable(t *testing.T) {
	t.Setenv(envWriteTimeout, "1hour")

	var cfg web.Config
	err := cfg.Finalize(testPrefix)
	if err == nil {
		t.Fatal("Finalize returned nil for a malformed duration override")
	}
	if !strings.Contains(err.Error(), envWriteTimeout) {
		t.Errorf("error = %v, want it to name %s", err, envWriteTimeout)
	}
}

func TestConfig_FinalizeMalformedPortNamesTheVariable(t *testing.T) {
	t.Setenv(envPort, "http")

	var cfg web.Config
	err := cfg.Finalize(testPrefix)
	if err == nil {
		t.Fatal("Finalize returned nil for a malformed port override")
	}
	if !strings.Contains(err.Error(), envPort) {
		t.Errorf("error = %v, want it to name %s", err, envPort)
	}
}

func TestConfig_FinalizeRejectsPortOutOfRange(t *testing.T) {
	cfg := web.Config{Port: new(70000)}
	if err := cfg.Finalize(""); err == nil {
		t.Fatal("Finalize returned nil for port 70000")
	}
}

func TestConfig_FinalizeRejectsNegativeTimeout(t *testing.T) {
	cfg := web.Config{ReadTimeout: dur(-time.Second)}
	if err := cfg.Finalize(""); err == nil {
		t.Fatal("Finalize returned nil for a negative read_timeout")
	}
}

func TestConfig_AddrBracketsIPv6(t *testing.T) {
	cfg := web.Config{Host: "::1", Port: new(8080)}
	if got, want := cfg.Addr(), "[::1]:8080"; got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
}

func TestConfig_LoadsThroughConfigLoad(t *testing.T) {
	dir := t.TempDir()
	body := `{"host":"127.0.0.1","port":9000,"read_timeout":"90s"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	cfg, err := config.Load[web.Config](config.Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.Addr(), "127.0.0.1:9000"; got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
	if time.Duration(*cfg.ReadTimeout) != 90*time.Second {
		t.Errorf("ReadTimeout = %s, want 1m30s", cfg.ReadTimeout)
	}
	if time.Duration(*cfg.IdleTimeout) != 2*time.Minute {
		t.Errorf("IdleTimeout = %s, want the default", cfg.IdleTimeout)
	}
}

func TestConfig_ZeroTimeoutFromFileSurvivesFinalize(t *testing.T) {
	dir := t.TempDir()
	body := `{"read_timeout":"0s"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	cfg, err := config.Load[web.Config](config.Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ReadTimeout == nil || time.Duration(*cfg.ReadTimeout) != 0 {
		t.Errorf("ReadTimeout = %v, want an explicit 0 (disabled), not the default", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout == nil || time.Duration(*cfg.WriteTimeout) != 15*time.Minute {
		t.Errorf("WriteTimeout = %v, want the default (unset in the file)", cfg.WriteTimeout)
	}
}

func TestConfig_ZeroTimeoutFromEnvSurvivesFinalize(t *testing.T) {
	t.Setenv(envReadTimeout, "0s")

	var cfg web.Config
	if err := cfg.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if cfg.ReadTimeout == nil || time.Duration(*cfg.ReadTimeout) != 0 {
		t.Errorf("ReadTimeout = %v, want an explicit 0 (disabled), not the default", cfg.ReadTimeout)
	}
}

func TestConfig_ExplicitPortZeroFromFileSurvivesFinalize(t *testing.T) {
	dir := t.TempDir()
	body := `{"port":0}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	cfg, err := config.Load[web.Config](config.Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Port == nil || *cfg.Port != 0 {
		t.Errorf("Port = %v, want an explicit 0 (ephemeral), not the default", cfg.Port)
	}
}

func TestConfig_ExplicitPortZeroFromEnvSurvivesFinalize(t *testing.T) {
	t.Setenv(envPort, "0")

	var cfg web.Config
	if err := cfg.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if cfg.Port == nil || *cfg.Port != 0 {
		t.Errorf("Port = %v, want an explicit 0 (ephemeral), not the default", cfg.Port)
	}
}

func TestConfig_FinalizeRejectsNegativePortFromEnv(t *testing.T) {
	t.Setenv(envPort, "-1")

	var cfg web.Config
	if err := cfg.Finalize(testPrefix); err == nil {
		t.Fatal("Finalize returned nil for port -1 from the environment")
	}
}
