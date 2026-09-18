package ratelimit_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware/rate-limit"
)

// Instantiating Load proves *ratelimit.Config satisfies the config.Config
// contract at compile time; the constraint cannot be written as an ordinary
// interface assertion because it carries a type element.
var _ = config.Load[ratelimit.Config]

// testPrefix is the env prefix override tests finalize with; the names below
// are what Finalize composes from it under the "rate_limit" block.
const (
	testPrefix  = "test"
	envRequests = "TEST_RATE_LIMIT_REQUESTS"
	envWindow   = "TEST_RATE_LIMIT_WINDOW"
)

// testBlock is the non-default block FinalizeBlock tests finalize with; the
// names below are what it composes from testPrefix and testBlock.
const (
	testBlock        = "login"
	envLoginRequests = "TEST_LOGIN_REQUESTS"
	envLoginWindow   = "TEST_LOGIN_WINDOW"
)

func dur(v time.Duration) *config.Duration {
	d := config.Duration(v)
	return &d
}

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", name)
		}
	}()
	fn()
}

// limited composes a handler answering 204 under a finalized limit of two
// requests per minute: long enough that no test crosses a window boundary.
func limited(t *testing.T) http.Handler {
	t.Helper()
	cfg := ratelimit.Config{Requests: new(2), Window: dur(time.Minute)}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	return web.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), ratelimit.New(cfg))
}

// get serves one GET from remoteAddr through handler and returns the
// recorder. httptest.NewRequest sets its own RemoteAddr, so the test's is
// set on top.
func get(handler http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	req.RemoteAddr = remoteAddr
	handler.ServeHTTP(rec, req)
	return rec
}

// Requests within the limit reach the handler; the one past it is answered
// with a 429 problem document carrying httprate's Retry-After.
func TestNew_RequestOverTheLimitIsA429(t *testing.T) {
	handler := limited(t)

	for i := 1; i <= 2; i++ {
		if rec := get(handler, "192.0.2.1:1234"); rec.Code != http.StatusNoContent {
			t.Fatalf("request %d: status = %d, want 204", i, rec.Code)
		}
	}
	rec := get(handler, "192.0.2.1:1234")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != web.ProblemMediaType {
		t.Errorf("Content-Type = %q, want %q", got, web.ProblemMediaType)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want the window in whole seconds, 60", got)
	}
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "2" {
		t.Errorf("X-RateLimit-Limit = %q, want 2", got)
	}
	var p web.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.Status != http.StatusTooManyRequests {
		t.Errorf("problem status = %d, want 429", p.Status)
	}
	if p.Type != web.ProblemTypeBlank {
		t.Errorf("problem type = %q, want %q", p.Type, web.ProblemTypeBlank)
	}
	if p.Instance != "/orders" {
		t.Errorf("problem instance = %q, want /orders", p.Instance)
	}
	if !strings.Contains(p.Detail, "2 requests per 1m0s") {
		t.Errorf("problem detail = %q, want it to name the configured limit", p.Detail)
	}
}

// The limit is per client: a second address is untouched by the first's
// exhausted budget.
func TestNew_LimitIsPerRemoteAddr(t *testing.T) {
	handler := limited(t)

	for range 3 {
		get(handler, "192.0.2.1:1234")
	}
	if rec := get(handler, "192.0.2.1:1234"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("first client: status = %d, want 429", rec.Code)
	}
	if rec := get(handler, "192.0.2.2:1234"); rec.Code != http.StatusNoContent {
		t.Errorf("second client: status = %d, want 204", rec.Code)
	}
}

// The port is not part of the key: one client on two source ports is one
// client.
func TestNew_PortIsNotPartOfTheKey(t *testing.T) {
	handler := limited(t)

	get(handler, "192.0.2.1:1234")
	get(handler, "192.0.2.1:5678")
	if rec := get(handler, "192.0.2.1:9012"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 across ports", rec.Code)
	}
}

// Two IPv6 addresses in the same /64 share one budget: the key is the
// canonicalized prefix, not the full address.
func TestNew_IPv6ClientsShareTheirSlash64(t *testing.T) {
	handler := limited(t)

	get(handler, "[2001:db8::1]:1234")
	get(handler, "[2001:db8::2]:1234")
	if rec := get(handler, "[2001:db8::3]:1234"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("same /64: status = %d, want 429", rec.Code)
	}
	if rec := get(handler, "[2001:db8:0:1::1]:1234"); rec.Code != http.StatusNoContent {
		t.Errorf("other /64: status = %d, want 204", rec.Code)
	}
}

// A RemoteAddr with no port is keyed as it is rather than rejected.
func TestNew_RemoteAddrWithoutPortIsKeyed(t *testing.T) {
	handler := limited(t)

	get(handler, "192.0.2.1")
	get(handler, "192.0.2.1")
	if rec := get(handler, "192.0.2.1"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	if rec := get(handler, "192.0.2.2"); rec.Code != http.StatusNoContent {
		t.Errorf("other client: status = %d, want 204", rec.Code)
	}
}

func TestNew_UnfinalizedConfigPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ratelimit.Config
	}{
		{"zero", ratelimit.Config{}},
		{"no window", ratelimit.Config{Requests: new(2)}},
		{"no requests", ratelimit.Config{Window: dur(time.Minute)}},
	} {
		mustPanic(t, "New("+tc.name+")", func() {
			ratelimit.New(tc.cfg)
		})
	}
}

func TestNew_InvalidConfigPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ratelimit.Config
	}{
		{"zero requests", ratelimit.Config{Requests: new(0), Window: dur(time.Minute)}},
		{"negative requests", ratelimit.Config{Requests: new(-1), Window: dur(time.Minute)}},
		{"zero window", ratelimit.Config{Requests: new(2), Window: dur(0)}},
		{"negative window", ratelimit.Config{Requests: new(2), Window: dur(-time.Second)}},
		{"sub-second window", ratelimit.Config{Requests: new(2), Window: dur(500 * time.Millisecond)}},
	} {
		mustPanic(t, "New("+tc.name+")", func() {
			ratelimit.New(tc.cfg)
		})
	}
}

func TestConfig_MergeSourceWinsWithoutClearing(t *testing.T) {
	base := ratelimit.Config{Requests: new(100), Window: dur(time.Minute)}
	base.Merge(&ratelimit.Config{Requests: new(50)})

	if *base.Requests != 50 {
		t.Errorf("Requests = %d, want 50 (the source sets it)", *base.Requests)
	}
	if time.Duration(*base.Window) != time.Minute {
		t.Errorf("Window = %s, want 1m (the source omits it and must not clear it)", base.Window)
	}

	base.Merge(&ratelimit.Config{Window: dur(time.Hour)})
	if *base.Requests != 50 {
		t.Errorf("Requests = %d, want 50 (the source omits it and must not clear it)", *base.Requests)
	}
	if time.Duration(*base.Window) != time.Hour {
		t.Errorf("Window = %s, want 1h (the source sets it)", base.Window)
	}
}

func TestConfig_FinalizeAppliesDefaults(t *testing.T) {
	var cfg ratelimit.Config
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if cfg.Requests == nil || *cfg.Requests != 300 {
		t.Errorf("Requests = %v, want 300", cfg.Requests)
	}
	if cfg.Window == nil || time.Duration(*cfg.Window) != time.Minute {
		t.Errorf("Window = %v, want 1m", cfg.Window)
	}
}

func TestConfig_FinalizeKeepsExplicitValues(t *testing.T) {
	cfg := ratelimit.Config{Requests: new(10), Window: dur(5 * time.Second)}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if *cfg.Requests != 10 {
		t.Errorf("Requests = %d, want 10", *cfg.Requests)
	}
	if time.Duration(*cfg.Window) != 5*time.Second {
		t.Errorf("Window = %s, want 5s", cfg.Window)
	}
}

func TestConfig_FinalizeEnvOverridesFiles(t *testing.T) {
	t.Setenv(envRequests, "42")
	t.Setenv(envWindow, "45s")

	cfg := ratelimit.Config{Requests: new(10), Window: dur(time.Minute)}
	if err := cfg.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if *cfg.Requests != 42 {
		t.Errorf("Requests = %d, want 42", *cfg.Requests)
	}
	if time.Duration(*cfg.Window) != 45*time.Second {
		t.Errorf("Window = %s, want 45s", cfg.Window)
	}
}

func TestConfig_FinalizeEmptyEnvValueLeavesConfigured(t *testing.T) {
	// An empty variable reads as unset, not as a request to clear the value.
	t.Setenv(envRequests, "")
	t.Setenv(envWindow, "")

	cfg := ratelimit.Config{Requests: new(10), Window: dur(30 * time.Second)}
	if err := cfg.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if *cfg.Requests != 10 {
		t.Errorf("Requests = %d, want 10", *cfg.Requests)
	}
	if time.Duration(*cfg.Window) != 30*time.Second {
		t.Errorf("Window = %s, want 30s", cfg.Window)
	}
}

func TestConfig_FinalizeEmptyPrefixSkipsOverrides(t *testing.T) {
	// An empty prefix composes no variable names, so nothing in the
	// environment applies: the hermetic form.
	t.Setenv(envRequests, "42")
	t.Setenv(envWindow, "45s")

	var cfg ratelimit.Config
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if *cfg.Requests != 300 || time.Duration(*cfg.Window) != time.Minute {
		t.Errorf("Requests = %d, Window = %s, want the defaults with a zero Env", *cfg.Requests, cfg.Window)
	}
}

func TestConfig_FinalizeMalformedRequestsNamesTheVariable(t *testing.T) {
	t.Setenv(envRequests, "many")

	var cfg ratelimit.Config
	err := cfg.Finalize(testPrefix)
	if err == nil {
		t.Fatal("Finalize returned nil for a malformed requests override")
	}
	if !strings.Contains(err.Error(), envRequests) {
		t.Errorf("error = %v, want it to name %s", err, envRequests)
	}
}

func TestConfig_FinalizeMalformedWindowNamesTheVariable(t *testing.T) {
	t.Setenv(envWindow, "1hour")

	var cfg ratelimit.Config
	err := cfg.Finalize(testPrefix)
	if err == nil {
		t.Fatal("Finalize returned nil for a malformed window override")
	}
	if !strings.Contains(err.Error(), envWindow) {
		t.Errorf("error = %v, want it to name %s", err, envWindow)
	}
}

func TestConfig_FinalizeRejectsNonPositiveRequests(t *testing.T) {
	for _, n := range []int{0, -1} {
		cfg := ratelimit.Config{Requests: new(n)}
		err := cfg.Finalize("")
		if err == nil {
			t.Fatalf("Finalize returned nil for requests %d", n)
		}
		if !strings.Contains(err.Error(), "requests") {
			t.Errorf("error = %v, want it to name requests", err)
		}
	}
}

func TestConfig_FinalizeRejectsNonPositiveRequestsFromEnv(t *testing.T) {
	t.Setenv(envRequests, "0")

	var cfg ratelimit.Config
	if err := cfg.Finalize(testPrefix); err == nil {
		t.Fatal("Finalize returned nil for requests 0 from the environment")
	}
}

func TestConfig_FinalizeRejectsSubSecondWindow(t *testing.T) {
	for _, d := range []time.Duration{-time.Second, 0, 999 * time.Millisecond} {
		cfg := ratelimit.Config{Window: dur(d)}
		err := cfg.Finalize("")
		if err == nil {
			t.Fatalf("Finalize returned nil for window %s", d)
		}
		if !strings.Contains(err.Error(), "window") {
			t.Errorf("error = %v, want it to name window", err)
		}
	}
}

func TestConfig_FinalizeAcceptsOneSecondWindow(t *testing.T) {
	cfg := ratelimit.Config{Window: dur(time.Second)}
	if err := cfg.Finalize(""); err != nil {
		t.Errorf("Finalize: %v, want a 1s window accepted", err)
	}
}

func TestNewEnv_ComposesNamesFromPrefixAndBlock(t *testing.T) {
	for _, tc := range []struct {
		block string
		want  ratelimit.Env
	}{
		{"rate_limit", ratelimit.Env{
			Requests: "HERALD_RATE_LIMIT_REQUESTS",
			Window:   "HERALD_RATE_LIMIT_WINDOW",
		}},
		{"login", ratelimit.Env{
			Requests: "HERALD_LOGIN_REQUESTS",
			Window:   "HERALD_LOGIN_WINDOW",
		}},
	} {
		t.Run(tc.block, func(t *testing.T) {
			if got := ratelimit.NewEnv("herald", tc.block); got != tc.want {
				t.Errorf("NewEnv(\"herald\", %q) = %+v, want %+v", tc.block, got, tc.want)
			}
		})
	}
}

func TestNewEnv_EmptyPrefixReturnsZeroEnv(t *testing.T) {
	if env := ratelimit.NewEnv("", "rate_limit"); env != (ratelimit.Env{}) {
		t.Errorf("NewEnv(\"\", \"rate_limit\") = %+v, want the zero Env (overrides disabled)", env)
	}
}

func TestConfig_FinalizeUsesRateLimitBlock(t *testing.T) {
	var cfg ratelimit.Config
	if err := cfg.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	want := ratelimit.Env{Requests: envRequests, Window: envWindow}
	if cfg.Env != want {
		t.Errorf("Env = %+v, want the \"rate_limit\" block names %+v", cfg.Env, want)
	}
}

func TestConfig_FinalizeBlockEnvOverridesOnlyItsOwnBlock(t *testing.T) {
	// The primary limit's variables are set alongside the login block's, so
	// a value leaking across blocks would show up as the wrong count.
	t.Setenv(envRequests, "300")
	t.Setenv(envLoginRequests, "5")
	t.Setenv(envLoginWindow, "10m")

	var login ratelimit.Config
	if err := login.FinalizeBlock(testPrefix, testBlock); err != nil {
		t.Fatalf("FinalizeBlock: %v", err)
	}
	if want := (ratelimit.Env{Requests: envLoginRequests, Window: envLoginWindow}); login.Env != want {
		t.Errorf("Env = %+v, want %+v", login.Env, want)
	}
	if *login.Requests != 5 {
		t.Errorf("Requests = %d, want 5", *login.Requests)
	}
	if time.Duration(*login.Window) != 10*time.Minute {
		t.Errorf("Window = %s, want 10m", login.Window)
	}

	var primary ratelimit.Config
	if err := primary.Finalize(testPrefix); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if *primary.Requests != 300 {
		t.Errorf("primary Requests = %d, want 300", *primary.Requests)
	}
	if time.Duration(*primary.Window) != time.Minute {
		t.Errorf("primary Window = %s, want the default (the override is the login block's)", primary.Window)
	}
}

func TestConfig_LoadsThroughConfigLoad(t *testing.T) {
	dir := t.TempDir()
	body := `{"requests":20,"window":"30s"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	cfg, err := config.Load[ratelimit.Config](config.Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if *cfg.Requests != 20 {
		t.Errorf("Requests = %d, want 20", *cfg.Requests)
	}
	if time.Duration(*cfg.Window) != 30*time.Second {
		t.Errorf("Window = %s, want 30s", cfg.Window)
	}
}
