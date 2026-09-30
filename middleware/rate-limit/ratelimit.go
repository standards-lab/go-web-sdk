package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/httprate"
	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-sdk"
)

const (
	defaultRequests = 300
	defaultWindow   = time.Minute
)

// Env records the environment-variable names [Config.FinalizeBlock]
// composed from its prefix and block: <BLOCK>_REQUESTS and <BLOCK>_WINDOW
// under the prefix. An empty prefix composes none, disabling the overrides.
type Env struct {
	Requests string
	Window   string
}

func newEnv(prefix, block string) Env {
	// go-core v0.5.0's EnvName returns "" for an empty prefix, which makes
	// this guard redundant once the pin moves.
	if prefix == "" {
		return Env{}
	}
	return Env{
		Requests: config.EnvName(prefix, block, "requests"),
		Window:   config.EnvName(prefix, block, "window"),
	}
}

// Config holds the limit, Requests per Window, loaded through go-core's
// config contract; nil fields take the default, 300 requests per minute.
type Config struct {
	Requests *int             `json:"requests"`
	Window   *config.Duration `json:"window"`
	Env      Env              `json:"-"`
}

// Merge overlays src's set fields onto the receiver.
func (c *Config) Merge(src *Config) {
	if src.Requests != nil {
		c.Requests = src.Requests
	}
	if src.Window != nil {
		c.Window = src.Window
	}
}

// Finalize is [Config.FinalizeBlock] under the block "rate_limit", the
// method [config.Load] calls.
func (c *Config) Finalize(envPrefix string) error {
	return c.FinalizeBlock(envPrefix, "rate_limit")
}

// FinalizeBlock composes [Env] from envPrefix and block, applies the
// defaults, then the environment overrides, and validates. A second limit, a
// tighter one on a login route say, finalizes under its own block.
func (c *Config) FinalizeBlock(envPrefix, block string) error {
	c.Env = newEnv(envPrefix, block)
	c.applyDefaults()
	if err := c.applyEnv(); err != nil {
		return err
	}
	return c.validate()
}

func (c *Config) applyDefaults() {
	if c.Requests == nil {
		c.Requests = new(defaultRequests)
	}
	if c.Window == nil {
		c.Window = new(config.Duration(defaultWindow))
	}
}

func (c *Config) applyEnv() error {
	if v := os.Getenv(c.Env.Requests); v != "" {
		requests, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Env.Requests, err)
		}
		c.Requests = &requests
	}
	return config.SetDurationFromEnv(&c.Window, c.Env.Window)
}

// validate rejects a non-positive request count and a window under one
// second: httprate reports the window to the client in whole seconds
// (Retry-After, X-RateLimit-Reset), so a shorter one cannot be expressed
// there.
func (c *Config) validate() error {
	if *c.Requests <= 0 {
		return fmt.Errorf("invalid requests: %d", *c.Requests)
	}
	if *c.Window < config.Duration(time.Second) {
		return fmt.Errorf("invalid window: %s", c.Window)
	}
	return nil
}

func (c *Config) finalized() bool {
	return c.Requests != nil && c.Window != nil
}

// New limits each client, keyed by its remote address with IPv6 reduced to
// its /64, to cfg.Requests per cfg.Window, answering a request over it with a
// 429 problem and Retry-After. It panics on a Config Finalize did not pass.
func New(cfg Config) web.Middleware {
	if !cfg.finalized() {
		panic("ratelimit: New requires a finalized Config; call Finalize first")
	}
	if err := cfg.validate(); err != nil {
		panic("ratelimit: New requires a valid Config: " + err.Error())
	}

	detail := fmt.Sprintf(
		"The client exceeded the limit of %d requests per %s.",
		*cfg.Requests, cfg.Window,
	)
	limited := func(w http.ResponseWriter, r *http.Request) {
		_ = web.WriteProblem(w, r, http.StatusTooManyRequests, "", detail)
	}

	return httprate.LimitBy(*cfg.Requests, cfg.Window.Duration(), keyByRemoteAddr,
		httprate.WithLimitHandler(limited), httprate.WithErrorHandler(failed))
}

// keyByRemoteAddr keys a request by the host part of its remote address,
// canonicalized through [httprate.CanonicalizeIP]. A RemoteAddr with no port
// (some test setups) is used as it is. The error return is httprate's
// signature; nothing here can fail.
func keyByRemoteAddr(r *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return httprate.CanonicalizeIP(host), nil
}

// failed answers httprate's error path, which neither the key function nor
// the in-memory counter reaches, with a fixed 500 problem.
func failed(w http.ResponseWriter, r *http.Request, _ error) {
	_ = web.WriteProblem(
		w,
		r,
		http.StatusInternalServerError,
		"",
		"The server could not evaluate the rate limit.",
	)
}
