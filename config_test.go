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
// are what Finalize composes from it under the "server" block.
const (
	testPrefix           = "test"
	envHost              = "TEST_SERVER_HOST"
	envPort              = "TEST_SERVER_PORT"
	envReadTimeout       = "TEST_SERVER_READ_TIMEOUT"
	envReadHeaderTimeout = "TEST_SERVER_READ_HEADER_TIMEOUT"
	envWriteTimeout      = "TEST_SERVER_WRITE_TIMEOUT"
	envIdleTimeout       = "TEST_SERVER_IDLE_TIMEOUT"
)

// testBlock is the non-default block FinalizeBlock tests finalize with; the
// names below are what it composes from testPrefix and testBlock.
const (
	testBlock                = "management"
	envMgmtHost              = "TEST_MANAGEMENT_HOST"
	envMgmtPort              = "TEST_MANAGEMENT_PORT"
	envMgmtReadTimeout       = "TEST_MANAGEMENT_READ_TIMEOUT"
	envMgmtReadHeaderTimeout = "TEST_MANAGEMENT_READ_HEADER_TIMEOUT"
	envMgmtWriteTimeout      = "TEST_MANAGEMENT_WRITE_TIMEOUT"
	envMgmtIdleTimeout       = "TEST_MANAGEMENT_IDLE_TIMEOUT"
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

func TestNewEnv_ComposesNamesFromPrefixAndBlock(t *testing.T) {
	for _, tc := range []struct {
		block string
		want  web.Env
	}{
		{"server", web.Env{
			Host:              "HERALD_SERVER_HOST",
			Port:              "HERALD_SERVER_PORT",
			ReadTimeout:       "HERALD_SERVER_READ_TIMEOUT",
			ReadHeaderTimeout: "HERALD_SERVER_READ_HEADER_TIMEOUT",
			WriteTimeout:      "HERALD_SERVER_WRITE_TIMEOUT",
			IdleTimeout:       "HERALD_SERVER_IDLE_TIMEOUT",
		}},
		{"management", web.Env{
			Host:              "HERALD_MANAGEMENT_HOST",
			Port:              "HERALD_MANAGEMENT_PORT",
			ReadTimeout:       "HERALD_MANAGEMENT_READ_TIMEOUT",
			ReadHeaderTimeout: "HERALD_MANAGEMENT_READ_HEADER_TIMEOUT",
			WriteTimeout:      "HERALD_MANAGEMENT_WRITE_TIMEOUT",
			IdleTimeout:       "HERALD_MANAGEMENT_IDLE_TIMEOUT",
		}},
	} {
		t.Run(tc.block, func(t *testing.T) {
			if got := web.NewEnv("herald", tc.block); got != tc.want {
				t.Errorf("NewEnv(\"herald\", %q) = %+v, want %+v", tc.block, got, tc.want)
			}
		})
	}
}

func TestNewEnv_EmptyPrefixReturnsZeroEnv(t *testing.T) {
	if env := web.NewEnv("", "server"); env != (web.Env{}) {
		t.Errorf("NewEnv(\"\", \"server\") = %+v, want the zero Env (overrides disabled)", env)
	}
}

func TestConfig_FinalizeUsesServerBlock(t *testing.T) {
	var cfg web.Config
	if err := cfg.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if got, want := cfg.Env, web.NewEnv(testPrefix, "server"); got != want {
		t.Errorf("Env = %+v, want the \"server\" block names %+v", got, want)
	}
}

func TestConfig_FinalizeBlockComposesNamesFromBlock(t *testing.T) {
	var cfg web.Config
	if err := cfg.FinalizeBlock(testPrefix, testBlock); err != nil {
		t.Fatalf("FinalizeBlock: %v", err)
	}

	want := web.Env{
		Host:              envMgmtHost,
		Port:              envMgmtPort,
		ReadTimeout:       envMgmtReadTimeout,
		ReadHeaderTimeout: envMgmtReadHeaderTimeout,
		WriteTimeout:      envMgmtWriteTimeout,
		IdleTimeout:       envMgmtIdleTimeout,
	}
	if cfg.Env != want {
		t.Errorf("Env = %+v, want %+v", cfg.Env, want)
	}
}

func TestConfig_FinalizeBlockNamesDoNotCollideAcrossBlocks(t *testing.T) {
	var primary, mgmt web.Config
	if err := primary.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if err := mgmt.FinalizeBlock(testPrefix, testBlock); err != nil {
		t.Fatalf("FinalizeBlock: %v", err)
	}

	for _, tc := range []struct {
		name    string
		primary string
		mgmt    string
	}{
		{"Host", primary.Env.Host, mgmt.Env.Host},
		{"Port", primary.Env.Port, mgmt.Env.Port},
		{"ReadTimeout", primary.Env.ReadTimeout, mgmt.Env.ReadTimeout},
		{"ReadHeaderTimeout", primary.Env.ReadHeaderTimeout, mgmt.Env.ReadHeaderTimeout},
		{"WriteTimeout", primary.Env.WriteTimeout, mgmt.Env.WriteTimeout},
		{"IdleTimeout", primary.Env.IdleTimeout, mgmt.Env.IdleTimeout},
	} {
		if tc.primary == tc.mgmt {
			t.Errorf("%s: both blocks compose %q under prefix %q", tc.name, tc.primary, testPrefix)
		}
	}
}

func TestConfig_FinalizeBlockEnvOverridesOnlyItsOwnBlock(t *testing.T) {
	// The primary server's variables are set alongside the management block's,
	// so a value leaking across blocks would show up as the wrong port.
	t.Setenv(envHost, "primary-env")
	t.Setenv(envPort, "9443")
	t.Setenv(envMgmtHost, "mgmt-env")
	t.Setenv(envMgmtPort, "9444")
	t.Setenv(envMgmtReadTimeout, "45s")

	mgmt := web.Config{Host: "from-file", Port: new(8081)}
	if err := mgmt.FinalizeBlock(testPrefix, testBlock); err != nil {
		t.Fatalf("FinalizeBlock: %v", err)
	}
	if mgmt.Host != "mgmt-env" {
		t.Errorf("Host = %q, want mgmt-env", mgmt.Host)
	}
	if *mgmt.Port != 9444 {
		t.Errorf("Port = %d, want 9444", *mgmt.Port)
	}
	if time.Duration(*mgmt.ReadTimeout) != 45*time.Second {
		t.Errorf("ReadTimeout = %s, want 45s", mgmt.ReadTimeout)
	}
	if time.Duration(*mgmt.WriteTimeout) != 15*time.Minute {
		t.Errorf("WriteTimeout = %s, want the default (no override set)", mgmt.WriteTimeout)
	}

	var primary web.Config
	if err := primary.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if primary.Host != "primary-env" {
		t.Errorf("primary Host = %q, want primary-env", primary.Host)
	}
	if *primary.Port != 9443 {
		t.Errorf("primary Port = %d, want 9443", *primary.Port)
	}
	if time.Duration(*primary.ReadTimeout) != time.Minute {
		t.Errorf("primary ReadTimeout = %s, want the default (the override is the management block's)", primary.ReadTimeout)
	}
}

func TestConfig_FinalizeBlockMalformedPortNamesTheBlockVariable(t *testing.T) {
	t.Setenv(envMgmtPort, "http")

	var cfg web.Config
	err := cfg.FinalizeBlock(testPrefix, testBlock)
	if err == nil {
		t.Fatal("FinalizeBlock returned nil for a malformed port override")
	}
	if !strings.Contains(err.Error(), envMgmtPort) {
		t.Errorf("error = %v, want it to name %s", err, envMgmtPort)
	}
}
